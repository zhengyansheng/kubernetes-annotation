/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package networkpolicy

import (
	"context"
	"reflect"
	"testing"
	"time"

	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	api "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/apis/networking"
	"k8s.io/kubernetes/pkg/features"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
)

func makeNetworkPolicy(isIngress, isEgress, hasEndPort bool) *networking.NetworkPolicy {

	protocolTCP := api.ProtocolTCP
	endPort := int32(32000)
	netPol := &networking.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar", Generation: 0},
		Spec: networking.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"a": "b"},
			},
		},
	}
	egress := networking.NetworkPolicyEgressRule{
		To: []networking.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"c": "d"},
				},
			},
		},
	}

	ingress := networking.NetworkPolicyIngressRule{
		From: []networking.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"c": "d"},
				},
			},
		},
	}

	ports := []networking.NetworkPolicyPort{
		{
			Protocol: &protocolTCP,
			Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: 31000},
		},
	}

	ingress.Ports = ports
	egress.Ports = ports

	if hasEndPort {
		ingress.Ports[0].EndPort = &endPort
		egress.Ports[0].EndPort = &endPort
	}

	if isIngress {
		netPol.Spec.Ingress = append(netPol.Spec.Ingress, ingress)
	}

	if isEgress {
		netPol.Spec.Egress = append(netPol.Spec.Egress, egress)
	}

	return netPol
}

func TestNetworkPolicyStrategy(t *testing.T) {

	// Create a Network Policy containing EndPort defined to compare with the generated by the tests
	netPol := makeNetworkPolicy(true, true, false)

	Strategy.PrepareForCreate(context.Background(), netPol)

	if netPol.Generation != 1 {
		t.Errorf("Create: Test failed. Network Policy Generation should be 1, got %d",
			netPol.Generation)
	}

	errs := Strategy.Validate(context.Background(), netPol)
	if len(errs) != 0 {
		t.Errorf("Unexpected error from validation for created Network Policy: %v", errs)
	}

	updatedNetPol := makeNetworkPolicy(true, true, true)
	updatedNetPol.ObjectMeta.SetResourceVersion("1")
	Strategy.PrepareForUpdate(context.Background(), updatedNetPol, netPol)

	errs = Strategy.ValidateUpdate(context.Background(), updatedNetPol, netPol)
	if len(errs) != 0 {
		t.Errorf("Unexpected error from validation for updated Network Policy: %v", errs)
	}

}

func TestNetworkPolicyStatusStrategy(t *testing.T) {
	for _, tc := range []struct {
		name              string
		enableFeatureGate bool
		invalidStatus     bool
	}{
		{
			name:              "Update NetworkPolicy status with FeatureGate enabled",
			enableFeatureGate: true,
			invalidStatus:     false,
		},
		{
			name:              "Update NetworkPolicy status with FeatureGate disabled",
			enableFeatureGate: false,
			invalidStatus:     false,
		},
		{
			name:              "Update NetworkPolicy status with FeatureGate enabled and invalid status",
			enableFeatureGate: true,
			invalidStatus:     true,
		},
		{
			name:              "Update NetworkPolicy status with FeatureGate disabled and invalid status",
			enableFeatureGate: false,
			invalidStatus:     true,
		},
	} {
		defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NetworkPolicyStatus, tc.enableFeatureGate)()
		ctx := genericapirequest.NewDefaultContext()
		if !StatusStrategy.NamespaceScoped() {
			t.Errorf("NetworkPolicy must be namespace scoped")
		}
		if StatusStrategy.AllowCreateOnUpdate() {
			t.Errorf("NetworkPolicy should not allow create on update")
		}

		oldNetPol := makeNetworkPolicy(false, true, false)
		newNetPol := makeNetworkPolicy(true, true, true)
		newNetPol.Status = networking.NetworkPolicyStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(networking.NetworkPolicyConditionStatusAccepted),
					Status:             metav1.ConditionTrue,
					Reason:             "RuleApplied",
					Message:            "rule was successfully applied",
					ObservedGeneration: 2,
				},
			},
		}
		if !tc.invalidStatus {
			newNetPol.Status.Conditions[0].LastTransitionTime = metav1.Time{Time: time.Now().Add(-5 * time.Minute)}
		}

		StatusStrategy.PrepareForUpdate(ctx, newNetPol, oldNetPol)
		if tc.enableFeatureGate {
			if !reflect.DeepEqual(oldNetPol.Spec, newNetPol.Spec) {
				t.Errorf("status update should not change network policy spec")
			}
			if len(newNetPol.Status.Conditions) != 1 {
				t.Fatalf("expecting 1 condition in network policy, got %d", len(newNetPol.Status.Conditions))
			}

			if newNetPol.Status.Conditions[0].Type != string(networking.NetworkPolicyConditionStatusAccepted) {
				t.Errorf("NetworkPolicy status updates should allow change of condition fields")
			}
		} else {
			if len(newNetPol.Status.Conditions) != 0 && !tc.enableFeatureGate {
				t.Fatalf("expecting 0 condition in network policy, got %d", len(newNetPol.Status.Conditions))
			}
		}

		errs := StatusStrategy.ValidateUpdate(ctx, newNetPol, oldNetPol)
		if tc.enableFeatureGate {
			if tc.invalidStatus && len(errs) == 0 {
				t.Error("invalid network policy status wasn't proper validated")
			}
			if !tc.invalidStatus && len(errs) > 0 {
				t.Errorf("valid network policy status returned an error: %v", errs)
			}
		} else {
			if len(errs) != 0 {
				t.Errorf("Unexpected error with disabled featuregate: %v", errs)
			}
		}

	}
}

// This test will verify the behavior of NetworkPolicy Status when enabling/disabling/re-enabling the feature gate
func TestNetworkPolicyStatusStrategyEnablement(t *testing.T) {
	// Enable the Feature Gate during the first rule creation
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NetworkPolicyStatus, true)()
	ctx := genericapirequest.NewDefaultContext()

	oldNetPol := makeNetworkPolicy(false, true, false)
	newNetPol := makeNetworkPolicy(true, true, false)
	newNetPol.Status = networking.NetworkPolicyStatus{
		Conditions: []metav1.Condition{
			{
				Type:               string(networking.NetworkPolicyConditionStatusAccepted),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Time{Time: time.Now().Add(-5 * time.Minute)},
				Reason:             "RuleApplied",
				Message:            "rule was successfully applied",
				ObservedGeneration: 2,
			},
		},
	}

	StatusStrategy.PrepareForUpdate(ctx, newNetPol, oldNetPol)

	if !reflect.DeepEqual(oldNetPol.Spec, newNetPol.Spec) {
		t.Errorf("status update should not change network policy spec")
	}

	if len(newNetPol.Status.Conditions) != 1 || newNetPol.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Error("expected network policy status is incorrect")
	}

	// Now let's disable the Feature Gate, update some other field from NetPol and expect the Status is already present
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NetworkPolicyStatus, false)()

	oldNetPol = newNetPol.DeepCopy()
	// 1 - It should not allow to change status, and just copy between objects when FG is disabled
	newNetPol.Status.Conditions[0].Status = metav1.ConditionFalse

	StatusStrategy.PrepareForUpdate(ctx, newNetPol, oldNetPol)
	if len(newNetPol.Status.Conditions) != 1 {
		t.Fatalf("expected conditions after disabling feature is invalid: got %d and expected 1", len(newNetPol.Status.Conditions))
	}

	if newNetPol.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Error("condition status changed with feature gate disabled")
	}

	oldNetPol = newNetPol.DeepCopy()
	// 2 - It should clear status if it contained previous data and is disabled now
	newNetPol.Status = networking.NetworkPolicyStatus{}
	StatusStrategy.PrepareForUpdate(ctx, newNetPol, oldNetPol)
	if len(newNetPol.Status.Conditions) != 0 {
		t.Errorf("expected conditions after disabling feature and cleaning status is invalid: got %d and expected 0", len(newNetPol.Status.Conditions))
	}

	oldNetPol = newNetPol.DeepCopy()
	// 3 - It should allow again to add status when re-enabling the FG
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NetworkPolicyStatus, true)()

	newNetPol.Status = networking.NetworkPolicyStatus{
		Conditions: []metav1.Condition{
			{
				Type:               string(networking.NetworkPolicyConditionStatusAccepted),
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Time{Time: time.Now().Add(-5 * time.Minute)},
				Reason:             "RuleApplied",
				Message:            "rule was successfully applied",
				ObservedGeneration: 2,
			},
		},
	}

	StatusStrategy.PrepareForUpdate(ctx, newNetPol, oldNetPol)

	if len(newNetPol.Status.Conditions) != 1 || newNetPol.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Error("expected network policy status is incorrect")
	}

}
