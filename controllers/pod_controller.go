/*

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

package controllers

import (
	"context"
	"fmt"
	"net/url"
	"path"

	"github.com/go-logr/logr"
	spiffeidv1beta1 "github.com/transferwise/spire-k8s-registrar/api/v1beta1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type PodReconcilerMode int32

const (
	PodReconcilerModeServiceAccount PodReconcilerMode = iota
	PodReconcilerModeLabel
	PodReconcilerModeAnnotation
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	TrustDomain string
	Mode        PodReconcilerMode
	Value       string
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch

// Reconcile creates a new SPIFFE ID when pods are created
func (r *PodReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("pod", req.NamespacedName)

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "unable to fetch Pod")
		}
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	spiffeidname := fmt.Sprintf("spire-operator-%s", pod.GetUID())

	spiffeId := ""
	switch r.Mode {
	case PodReconcilerModeServiceAccount:
		spiffeId = r.makeID("ns/%s/sa/%s", req.Namespace, pod.Spec.ServiceAccountName)
	case PodReconcilerModeLabel:
		if val, ok := pod.GetLabels()[r.Value]; ok {
			spiffeId = r.makeID("%s", val)
		} else {
			// No relevant label
			return ctrl.Result{}, nil
		}
	case PodReconcilerModeAnnotation:
		if val, ok := pod.GetAnnotations()[r.Value]; ok {
			spiffeId = r.makeID("%s", val)
		} else {
			// No relevant annotation
			return ctrl.Result{}, nil
		}
	}

	existing := &spiffeidv1beta1.SpiffeID{}
	if err := r.Get(ctx, types.NamespacedName{Name: spiffeidname}, existing); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "Failed to get SpiffeID", "name", spiffeidname)
			return ctrl.Result{}, err
		}
	}

	namespaceSpiffeId := &spiffeidv1beta1.SpiffeID{
		ObjectMeta: v1.ObjectMeta{
			Name:        spiffeidname,
			Namespace:   pod.Namespace,
			Annotations: make(map[string]string),
		},
		Spec: spiffeidv1beta1.SpiffeIDSpec{
			SpiffeId: spiffeId,
			DnsNames: make([]string, 0),
			Selector: spiffeidv1beta1.Selector{
				PodUid:    pod.GetUID(),
				Namespace: pod.Namespace,
			},
		},
	}

	namespaceSpiffeId.Spec.DnsNames = append(namespaceSpiffeId.Spec.DnsNames, pod.Name)

	err := controllerutil.SetControllerReference(&pod, namespaceSpiffeId, r.Scheme)
	if err != nil {
		log.Error(err, "Failed to create new SpiffeID", "SpiffeID.Name", namespaceSpiffeId.Name)
		return ctrl.Result{}, err
	}
	err = r.Create(ctx, namespaceSpiffeId)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			log.Error(err, "Failed to create new SpiffeID", "SpiffeID.Name", namespaceSpiffeId.Name)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

func (r *PodReconciler) makeID(pathFmt string, pathArgs ...interface{}) string {
	id := url.URL{
		Scheme: "spiffe",
		Host:   r.TrustDomain,
		Path:   path.Clean(fmt.Sprintf(pathFmt, pathArgs...)),
	}
	return id.String()
}

// EndPointReconciler reconciles a EndPoint object
type EndpointReconciler struct {
	client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	spiffeIDCollection map[string]string
}

// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=endpoints/status,verbs=get;update;patch

func (e *EndpointReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	var endpoints corev1.Endpoints

	ctx := context.Background()
	log := e.Log.WithValues("endpoint", req.NamespacedName)

	if err := e.Get(ctx, req.NamespacedName, &endpoints); err != nil {
		if errors.IsNotFound(err) {
			// Delete event
			if err := e.deleteExternalResources(ctx, log, req.NamespacedName); err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		log.Error(err, "unable to fetch Endpoints")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	svcName := req.NamespacedName.Name + "." + req.NamespacedName.Namespace
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			if address.TargetRef != nil {
				spiffeidname := fmt.Sprintf("spire-operator-%s", address.TargetRef.UID)
				existing := &spiffeidv1beta1.SpiffeID{}
				if err := e.Get(ctx, types.NamespacedName{Name: spiffeidname, Namespace: address.TargetRef.Namespace}, existing); err != nil {
					if !errors.IsNotFound(err) {
						log.Error(err, "failed to get SpiffeID", "name", spiffeidname)
						return ctrl.Result{}, err
					}
					continue
				}
				if existing != nil {
					log.Info("adding svc name to dns names entry", "service", svcName)
					if !containsString(existing.Spec.DnsNames, svcName) {
						existing.Spec.DnsNames = append([]string{svcName}, existing.Spec.DnsNames...)
						e.spiffeIDCollection[svcName] = spiffeidname
						if err := e.Update(ctx, existing); err != nil {
							log.Error(err, "unable to add DNS names in SPIFFE ID CRD", "name", spiffeidname)
							return ctrl.Result{}, err
						}
					}
				}
			}
		}
	}

	return ctrl.Result{}, nil
}
func (e *EndpointReconciler) deleteExternalResources(ctx context.Context, log logr.Logger, namespacedName types.NamespacedName) error {
	svcName := namespacedName.Name + "." + namespacedName.Namespace
	spiffeidname := e.spiffeIDCollection[svcName]

	existing := &spiffeidv1beta1.SpiffeID{}
	if err := e.Get(ctx, types.NamespacedName{Name: spiffeidname, Namespace: namespacedName.Namespace}, existing); err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "failed to get SpiffeID", "name", spiffeidname)
			return err
		}

		return nil
	}
	if existing != nil {
		log.Info("deleting DNS names for", "service", svcName)
		i := 0 // output index
		for _, dnsName := range existing.Spec.DnsNames {
			if dnsName != svcName {
				// copy and increment index
				existing.Spec.DnsNames[i] = dnsName
				i++
			}
		}

		existing.Spec.DnsNames = existing.Spec.DnsNames[:i]
		if err := e.Update(ctx, existing); err != nil {
			log.Error(err, "unable to delete DNS names in SPIFFE ID CRD", "name", spiffeidname)
			return err
		}

		delete(e.spiffeIDCollection, svcName)
	}

	return nil
}

func (e *EndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	e.spiffeIDCollection = make(map[string]string)
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{}).
		Complete(e)
}
