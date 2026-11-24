package lbwatcher

import (
	"testing"

	k8sv1alpha1 "github.com/netrisai/netris-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestManagesService(t *testing.T) {
	class, other := "netris.ai/l4lb", "example.com/other"
	for _, tc := range []struct {
		name            string
		configuredClass string
		serviceClass    *string
		serviceType     v1.ServiceType
		want            bool
	}{
		{"default accepts classless", "", nil, v1.ServiceTypeLoadBalancer, true},
		{"default rejects named class", "", &class, v1.ServiceTypeLoadBalancer, false},
		{"named accepts matching class", class, &class, v1.ServiceTypeLoadBalancer, true},
		{"named rejects other class", class, &other, v1.ServiceTypeLoadBalancer, false},
		{"named rejects classless", class, nil, v1.ServiceTypeLoadBalancer, false},
		{"rejects non-LoadBalancer", "", nil, v1.ServiceTypeClusterIP, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &Watcher{Options: Options{LoadBalancerClass: tc.configuredClass}}
			spec := v1.ServiceSpec{Type: tc.serviceType, LoadBalancerClass: tc.serviceClass}
			if got := w.managesService(spec); got != tc.want {
				t.Errorf("managesService() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadBalancerCleanupIsScopedToClass(t *testing.T) {
	class := "netris.ai/l4lb"
	lbs := []k8sv1alpha1.L4LB{
		{ObjectMeta: metav1.ObjectMeta{Name: "legacy", Annotations: map[string]string{}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "named", Annotations: map[string]string{loadBalancerClassAnnotation: class}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "manual"}},
	}
	for i := range lbs[:2] {
		lbs[i].SetServiceName(lbs[i].Name)
		lbs[i].SetServiceNamespace("default")
		lbs[i].SetServiceUID(lbs[i].Name)
	}

	for _, tc := range []struct{ class, wantDeleted string }{
		{"", "legacy"},
		{class, "named"},
	} {
		t.Run(tc.wantDeleted, func(t *testing.T) {
			w := &Watcher{Options: Options{LoadBalancerClass: tc.class}}
			_, _, deleted, _ := compareLoadBalancers(w.filterL4LBs(lbs), nil)
			if len(deleted) != 1 || deleted[0].Name != tc.wantDeleted {
				t.Fatalf("expected only %s to be deleted, got %v", tc.wantDeleted, deleted)
			}
		})
	}
}
