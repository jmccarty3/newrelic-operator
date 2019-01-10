package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/newrelic-cli/newrelic"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ CRD = &Monitor{}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type MonitorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Monitor `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Monitor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              MonitorSpec   `json:"spec"`
	Status            MonitorStatus `json:"status,omitempty"`
}

type MonitorSpec struct {
	Type          *string              `json:"type,omitempty"`
	Frequency     *int64               `json:"frequency,omitempty"`
	URI           *string              `json:"uri,omitempty"`
	Locations     []*string            `json:"locations,omitempty"`
	Status        *MonitorStatusString `json:"status,omitempty"`
	SLAThreshold  *float64             `json:"slaThreshold,omitempty"`
	ManageUpdates *bool                `json:"manageUpdates,omitempty"`
	Options       MonitorOptions       `json:"options,omitempty"`
	Script        *Script              `json:"script,omitempty"`
	Conditions    []Conditions         `json:"conditions,omitempty"`
}

type MonitorStatus struct {
	Status
	Policies []int64 `json:"policies,omitempty"`
}

func (s MonitorStatus) IsCreated() bool {
	return s.ID != nil
}

func (s MonitorStatus) GetSum() []byte {
	return s.Hash
}

func (s MonitorStatus) SetSum(data []byte) {
	s.Hash = data
}

type MonitorStatusString string

const (
	Enabled  MonitorStatusString = "enabled"
	Disabled MonitorStatusString = "disabled"
	Muted    MonitorStatusString = "muted"
)

func (s MonitorStatusString) String() string {
	return string(s)
}

type MonitorOptions struct {
	ValidationString       *string `json:"validationString,omitempty"`
	VerifySSL              bool    `json:"verifySSL,omitempty"`
	BypassHEADRequest      bool    `json:"bypassHEADRequest,omitempty"`
	TreatRedirectAsFailure bool    `json:"treatRedirectAsFailure,omitempty"`
}

type Conditions struct {
	PolicyName string  `json:"policyName,omitempty"`
	RunbookURL *string `json:"runbookURL,omitempty"`
}

type Script struct {
	ScriptText *string `json:"scriptText,omitempty"`
}

func (s MonitorSpec) GetSum() []byte {
	b, err := json.Marshal(s)
	if err != nil {
		log.Error("unable to marshal Monitor")
		return nil
	}
	return sum(b)
}

// IsCreated specifies if the object has been created in new relic yet
func (s *Monitor) IsCreated() bool {
	return s.Status.IsCreated()
}

// Signature for the CRD
func (s *Monitor) Signature() string {
	return fmt.Sprintf("%s %s/%s", s.TypeMeta.Kind, s.Namespace, s.Name)
}

func (s *Monitor) HasChanged() bool {
	return hasChanged(&s.Spec, &s.Status)
}

func (s *Monitor) toNewRelic() *newrelic.Monitor {
	data := &newrelic.Monitor{
		Name:         &s.ObjectMeta.Name,
		Type:         s.Spec.Type,
		Frequency:    s.Spec.Frequency,
		URI:          s.Spec.URI,
		Locations:    s.Spec.Locations,
		SLAThreshold: s.Spec.SLAThreshold,
		Script:       &newrelic.Script{},
		Options: newrelic.MonitorOptions{
			ValidationString:       s.Spec.Options.ValidationString,
			VerifySSL:              s.Spec.Options.VerifySSL,
			BypassHEADRequest:      s.Spec.Options.BypassHEADRequest,
			TreatRedirectAsFailure: s.Spec.Options.TreatRedirectAsFailure,
		},
	}

	if s.Spec.Script != nil {
		data.Script.ScriptText = s.Spec.Script.ScriptText
	}

	if s.Spec.Status != nil {
		status := s.Spec.Status.String()
		data.Status = &status
	}

	if data.Type == nil {
		val := "simple"
		data.Type = &val
	}

	if data.Frequency == nil {
		val := int64(10)
		data.Frequency = &val
	}

	if data.Locations == nil {
		val := "AWS_US_WEST_1"
		data.Locations = []*string{
			&val,
		}
	}

	if data.Status == nil {
		val := Enabled.String()
		data.Status = &val
	}

	return data
}

// Create in newrelic
func (s *Monitor) Create(ctx context.Context) error {
	data, rsp, err := clientSythetics.SyntheticsMonitors.Create(ctx, s.toNewRelic())
	err = handleError(rsp, err)
	if err != nil {
		s.Status.Info = err.Error()
		return err
	}

	s.Status.ID = data.ID
	s.Status.Info = "Created"
	s.Status.Hash = s.Spec.GetSum()

	s.SetFinalizers([]string{finalizer})

	err = s.updateCondition(ctx)
	if err != nil {
		s.Status.Info = err.Error()
		return err
	}

	return nil
}

// Delete in newrelic
func (s *Monitor) Delete(ctx context.Context) error {
	id := s.Status.ID
	if id == nil {
		return fmt.Errorf("alert Policy object has not been created %s", s.ObjectMeta.Name)
	}

	if s.Status.Policies != nil {
		for _, item := range s.Status.Policies {
			rsp, err := client.AlertsConditions.DeleteByID(ctx, newrelic.ConditionSynthetics, item)
			err = handleError(rsp, err)
			if err != nil {
				return err
			}
		}
	}

	rsp, err := clientSythetics.SyntheticsMonitors.DeleteByID(ctx, id)
	err = handleError(rsp, err)
	if err != nil {
		return err
	}

	return nil
}

// GetID for the new relic object
func (s *Monitor) GetID() string {
	if s.Status.ID != nil {
		return *s.Status.ID
	}
	return ""
}

// Update object in newrelic
func (s *Monitor) Update(ctx context.Context) error {
	if s.Spec.ManageUpdates == nil || (s.Spec.ManageUpdates != nil && *(s.Spec.ManageUpdates)) {
		rsp, err := clientSythetics.SyntheticsMonitors.Update(ctx, s.toNewRelic(), s.Status.ID)
		err = handleError(rsp, err)
		if err != nil {
			s.Status.Info = err.Error()
			return err
		}

		err = s.updateCondition(ctx)
		if err != nil {
			s.Status.Info = err.Error()
			return err
		}

		s.Status.Hash = s.Spec.GetSum()
	} else {
		log.Warn(fmt.Sprintf("Skipping update for %s", s.Signature()))
	}
	return nil
}

func (s *Monitor) updateCondition(ctx context.Context) error {
	if s.Spec.Conditions != nil {
		oldPolicies := s.Status.Policies

		s.Status.Policies = []int64{}
		for index, item := range s.Spec.Conditions {

			cond := &newrelic.AlertsConditionEntity{
				AlertsSyntheticsConditionEntity: &newrelic.AlertsSyntheticsConditionEntity{
					AlertsSyntheticsCondition: &newrelic.AlertsSyntheticsCondition{
						Name:       &s.Name,
						MonitorID:  s.Status.ID,
						RunbookURL: item.RunbookURL,
					},
				},
			}

			var data *newrelic.AlertsConditionEntity
			var rsp *newrelic.Response
			var err error
			if len(oldPolicies) > index {
				data, rsp, err = client.AlertsConditions.Update(ctx, newrelic.ConditionSynthetics, cond, oldPolicies[index])
			} else {
				policyID, err := s.findPolicyId(ctx, item.PolicyName)
				if err != nil {
					s.Status.Info = err.Error()
					return err
				}
				data, rsp, err = client.AlertsConditions.Create(ctx, newrelic.ConditionSynthetics, cond, *policyID)
			}

			if data != nil {
				conditionID := *data.AlertsSyntheticsConditionEntity.AlertsSyntheticsCondition.ID
				s.Status.Policies = append(s.Status.Policies, conditionID)
			}
			err = handleError(rsp, err)
			if err != nil {
				s.Status.Info = err.Error()
				return err
			}

		}

		if len(oldPolicies) > len(s.Status.Policies) {
			for _, item := range oldPolicies[len(s.Status.Policies):len(oldPolicies)] {
				rsp, err := client.AlertsConditions.DeleteByID(ctx, newrelic.ConditionSynthetics, item)
				err = handleErrorMessage("delete error %v", rsp, err)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// func (s *Monitor) deleteDuplicate(ctx context.Context) error {
// 	data, rsp, err := clientSythetics.SyntheticsMonitors.ListAll(ctx, &newrelic.MonitorListOptions{})
// 	err = handleError(rsp, err)
// 	if err != nil {
// 		s.Status.Info = err.Error()
// 		return err
// 	}

// 	for _, item := range data.Monitors {
// 		if s.Name == *item.Name {
// 			rsp, err := clientSythetics.SyntheticsMonitors.DeleteByID(ctx, item.ID)
// 			err = handleError(rsp, err)
// 			if err != nil {
// 				return err
// 			}
// 		}
// 	}

// 	return nil
// }

func (s *Monitor) findPolicyId(ctx context.Context, name string) (*int64, error) {
	policies, rsp, err := client.AlertsPolicies.ListAll(ctx, &newrelic.AlertsPolicyListOptions{
		NameOptions: name,
	})
	err = handleError(rsp, err)
	if err != nil {
		s.Status.Info = err.Error()
		return nil, err
	}

	if len(policies.AlertsPolicies) != 1 {
		err = fmt.Errorf("expected a policy search by name to only return 1 result, but recieved %d for %s", len(policies.AlertsPolicies), name)
		s.Status.Info = err.Error()
		return nil, err
	}

	return policies.AlertsPolicies[0].ID, nil
}

func init() {
	SchemeBuilder.Register(&Monitor{}, &MonitorList{})
}
