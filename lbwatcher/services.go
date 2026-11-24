/*
Copyright 2021. Netris, Inc.

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

package lbwatcher

import (
	"context"
	"fmt"

	k8sv1alpha1 "github.com/netrisai/netris-operator/api/v1alpha1"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getServices(clientset *kubernetes.Clientset, namespace string) (*v1.ServiceList, error) {
	ctx, cancel := context.WithTimeout(cntxt, contextTimeout)
	defer cancel()
	services, err := clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return services, fmt.Errorf("{getServices} %s", err)
	}
	return services, nil
}

func (w *Watcher) managesService(spec v1.ServiceSpec) bool {
	if spec.Type != v1.ServiceTypeLoadBalancer {
		return false
	}
	if w.Options.LoadBalancerClass == "" {
		return spec.LoadBalancerClass == nil
	}
	return spec.LoadBalancerClass != nil && *spec.LoadBalancerClass == w.Options.LoadBalancerClass
}

func (w *Watcher) assignIngress(clientset *kubernetes.Clientset, ips []string, lb *k8sv1alpha1.L4LB) (*v1.Service, error) {
	namespace, name := lb.GetServiceNamespace(), lb.GetServiceName()
	ctx, cancel := context.WithTimeout(cntxt, contextTimeout)
	defer cancel()

	var ingressList []v1.LoadBalancerIngress

	for _, ip := range ips {
		ingressList = append(ingressList, v1.LoadBalancerIngress{IP: ip})
	}

	var updatedService *v1.Service

	service, err := clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return updatedService, err
	}

	if !w.managesService(service.Spec) || string(service.UID) != lb.GetServiceUID() {
		return nil, nil
	}

	service.Status = v1.ServiceStatus{LoadBalancer: v1.LoadBalancerStatus{Ingress: ingressList}}

	updatedService, updateErr := clientset.CoreV1().Services(namespace).UpdateStatus(context.TODO(), service, metav1.UpdateOptions{})

	return updatedService, updateErr
}

func eventRecorder(kubeClient *kubernetes.Clientset) (record.EventRecorder, watch.Interface, record.EventBroadcaster) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logrus.New().Debugf)
	w := eventBroadcaster.StartRecordingToSink(
		&typedcorev1.EventSinkImpl{
			Interface: kubeClient.CoreV1().Events(""),
		},
	)

	recorder := eventBroadcaster.NewRecorder(
		scheme.Scheme,
		v1.EventSource{},
	)

	return recorder, w, eventBroadcaster
}

func (w *Watcher) createEvent(clientset *kubernetes.Clientset, recorder record.EventRecorder, lb *k8sv1alpha1.L4LB) error {
	namespace, name := lb.GetServiceNamespace(), lb.GetServiceName()
	ctx, cancel := context.WithTimeout(cntxt, contextTimeout)
	defer cancel()
	service, err := clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("{createEvent GetService} %s", err)
		}
		return nil
	}

	if !w.managesService(service.Spec) || string(service.UID) != lb.GetServiceUID() {
		return nil
	}

	ref, err := reference.GetReference(scheme.Scheme, service)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("{createEvent GetReference} %s", err)
		}
		return nil
	}
	recorder.Event(ref, v1.EventTypeWarning, lb.Status.Status, lb.Status.Message)

	return nil
}
