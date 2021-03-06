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
	"errors"
	"fmt"
	"net/url"
	"path"

	"github.com/go-logr/logr"
	"github.com/spiffe/spire/proto/spire/api/registration"
	"github.com/spiffe/spire/proto/spire/common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	spiffeidv1beta1 "github.com/transferwise/spire-k8s-registrar/api/v1beta1"
)

// SpiffeIDReconciler reconciles a SpiffeID object
type SpiffeIDReconciler struct {
	client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	myId               *string
	SpireClient        registration.RegistrationClient
	TrustDomain        string
	Cluster            string
	spiffeIDCollection map[string]string
}

// +kubebuilder:rbac:groups=spiffeid.spiffe.io,resources=spiffeids,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=spiffeid.spiffe.io,resources=spiffeids/status,verbs=get;update;patch

func (r *SpiffeIDReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("spireentry", req.NamespacedName)

	if r.myId == nil {
		if err := r.makeMyId(ctx, log); err != nil {
			log.Error(err, "unable to create parent ID")
			return ctrl.Result{}, err
		}
	}

	var spiffeID spiffeidv1beta1.SpiffeID
	if err := r.Get(ctx, req.NamespacedName, &spiffeID); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.Error(err, "unable to fetch SpiffeID")
			return ctrl.Result{}, err
		}

		// Delete event
		if err := r.ensureDeleted(ctx, log, r.spiffeIDCollection[req.NamespacedName.String()]); err != nil {
			log.Error(err, "unable to delete spire entry", "entryid", r.spiffeIDCollection[req.NamespacedName.String()])
			return ctrl.Result{}, err
		}
		delete(r.spiffeIDCollection, req.NamespacedName.String())
		log.Info("Finalized entry", "entry", req.NamespacedName.String())

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	entryId, err := r.getOrCreateSpiffeID(ctx, log, &spiffeID)
	if err != nil {
		log.Error(err, "unable to get or create spire entry", "request", req)
		return ctrl.Result{}, err
	}

	oldEntryId := spiffeID.Status.EntryId
	if oldEntryId == nil || *oldEntryId != entryId {
		// We need to update the Status field
		if oldEntryId != nil {
			// entry resource must have been modified, delete the hanging one
			if err := r.ensureDeleted(ctx, log, *spiffeID.Status.EntryId); err != nil {
				log.Error(err, "unable to delete old spire entry", "entryid", *spiffeID.Status.EntryId)
				return ctrl.Result{}, err
			}
		}
		spiffeID.Status.EntryId = &entryId
		if err := r.Status().Update(ctx, &spiffeID); err != nil {
			log.Error(err, "unable to update SpiffeID status")
			return ctrl.Result{}, err
		}
	}

	currentEntry, err := r.SpireClient.FetchEntry(ctx, &registration.RegistrationEntryID{Id: entryId})
	if err != nil {
		log.Error(err, "unable to fetch current SpiffeID entry")
		return ctrl.Result{}, err
	}

	if !equalStringSlice(currentEntry.DnsNames, spiffeID.Spec.DnsNames) {
		log.Info("Updating Spire Entry")

		_, err := r.SpireClient.UpdateEntry(ctx, &registration.UpdateEntryRequest{
			Entry: r.createCommonRegistrationEntry(spiffeID),
		})
		if err != nil {
			log.Error(err, "unable to update SpiffeID with new DNS names")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *SpiffeIDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.spiffeIDCollection = make(map[string]string)
	return ctrl.NewControllerManagedBy(mgr).
		For(&spiffeidv1beta1.SpiffeID{}).
		Complete(r)
}

func (r *SpiffeIDReconciler) ensureDeleted(ctx context.Context, reqLogger logr.Logger, entryId string) error {
	if _, err := r.SpireClient.DeleteEntry(ctx, &registration.RegistrationEntryID{Id: entryId}); err != nil {
		if status.Code(err) != codes.NotFound {
			if status.Code(err) == codes.Internal {
				// Spire server currently returns a 500 if delete fails due to the entry not existing. This is probably a bug.
				// We work around it by attempting to fetch the entry, and if it's not found then all is good.
				if _, err := r.SpireClient.FetchEntry(ctx, &registration.RegistrationEntryID{Id: entryId}); err != nil {
					if status.Code(err) == codes.NotFound {
						reqLogger.Info("Entry already deleted", "entry", entryId)
						return nil
					}
				}
			}
			return err
		}
	}
	reqLogger.Info("Deleted entry", "entry", entryId)
	return nil
}

// ServerURI creates a server SPIFFE URI given a trustDomain.
func ServerURI(trustDomain string) *url.URL {
	return &url.URL{
		Scheme: "spiffe",
		Host:   trustDomain,
		Path:   path.Join("spire", "server"),
	}
}

// ServerID creates a server SPIFFE ID string given a trustDomain.
func ServerID(trustDomain string) string {
	return ServerURI(trustDomain).String()
}

func (r *SpiffeIDReconciler) makeID(pathFmt string, pathArgs ...interface{}) string {
	id := url.URL{
		Scheme: "spiffe",
		Host:   r.TrustDomain,
		Path:   path.Clean(fmt.Sprintf(pathFmt, pathArgs...)),
	}
	return id.String()
}

func (r *SpiffeIDReconciler) nodeID() string {
	return r.makeID("spire-k8s-registrar/%s/node", r.Cluster)
}

func (r *SpiffeIDReconciler) makeMyId(ctx context.Context, reqLogger logr.Logger) error {
	myId := r.nodeID()
	reqLogger.Info("Initializing registrar parent ID.")
	_, err := r.SpireClient.CreateEntry(ctx, &common.RegistrationEntry{
		Selectors: []*common.Selector{
			{Type: "k8s_psat", Value: fmt.Sprintf("cluster:%s", r.Cluster)},
		},
		ParentId: ServerID(r.TrustDomain),
		SpiffeId: myId,
	})
	if err != nil {
		if status.Code(err) != codes.AlreadyExists {
			reqLogger.Info("Failed to create registrar parent ID", "spiffeID", myId)
			return err
		}
	}
	reqLogger.Info("Initialized registrar parent ID", "spiffeID", myId)
	r.myId = &myId
	return nil
}

var ExistingEntryNotFoundError = errors.New("no existing matching entry found")

func (r *SpiffeIDReconciler) getExistingEntry(ctx context.Context, reqLogger logr.Logger, id string, selectors []*common.Selector) (string, error) {
	entries, err := r.SpireClient.ListByParentID(ctx, &registration.ParentID{
		Id: *r.myId,
	})
	if err != nil {
		reqLogger.Error(err, "Failed to retrieve existing spire entry")
		return "", err
	}

	selectorMap := map[string]bool{}
	for _, sel := range selectors {
		key := sel.Type + ":" + sel.Value
		selectorMap[key] = true
	}
	for _, entry := range entries.Entries {
		if entry.GetSpiffeId() == id {
			if len(entry.GetSelectors()) != len(selectors) {
				continue
			}
			found := true
			for _, sel := range entry.GetSelectors() {
				key := sel.Type + ":" + sel.Value
				if _, ok := selectorMap[key]; !ok {
					found = false
					break
				}
			}
			if found {
				return entry.GetEntryId(), nil
			}
		}
	}
	return "", ExistingEntryNotFoundError
}

func (r *SpiffeIDReconciler) createCommonRegistrationEntry(instance spiffeidv1beta1.SpiffeID) *common.RegistrationEntry {
	selectors := make([]*common.Selector, 0, len(instance.Spec.Selector.PodLabel))
	for k, v := range instance.Spec.Selector.PodLabel {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("pod-label:%s:%s", k, v),
		})
	}
	if len(instance.Spec.Selector.PodName) > 0 {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("pod-name:%s", instance.Spec.Selector.PodName),
		})
	}
	if len(instance.Spec.Selector.PodUid) > 0 {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("pod-uid:%s", instance.Spec.Selector.PodUid),
		})
	}
	if len(instance.Spec.Selector.Namespace) > 0 {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("ns:%s", instance.Spec.Selector.Namespace),
		})
	}
	if len(instance.Spec.Selector.ServiceAccount) > 0 {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("sa:%s", instance.Spec.Selector.ServiceAccount),
		})
	}
	for _, v := range instance.Spec.Selector.Arbitrary {
		selectors = append(selectors, &common.Selector{Value: v})
	}

	return &common.RegistrationEntry{
		Selectors: selectors,
		ParentId:  *r.myId,
		SpiffeId:  instance.Spec.SpiffeId,
		DnsNames:  instance.Spec.DnsNames,
		EntryId:   *instance.Status.EntryId,
	}
}

func (r *SpiffeIDReconciler) getOrCreateSpiffeID(ctx context.Context, reqLogger logr.Logger, instance *spiffeidv1beta1.SpiffeID) (string, error) {

	// TODO: sanitize!
	selectors := make([]*common.Selector, 0, len(instance.Spec.Selector.PodLabel))
	for k, v := range instance.Spec.Selector.PodLabel {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("pod-label:%s:%s", k, v),
		})
	}
	if len(instance.Spec.Selector.PodName) > 0 {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("pod-name:%s", instance.Spec.Selector.PodName),
		})
	}
	if len(instance.Spec.Selector.PodUid) > 0 {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("pod-uid:%s", instance.Spec.Selector.PodUid),
		})
	}
	if len(instance.Spec.Selector.Namespace) > 0 {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("ns:%s", instance.Spec.Selector.Namespace),
		})
	}
	if len(instance.Spec.Selector.ServiceAccount) > 0 {
		selectors = append(selectors, &common.Selector{
			Type:  "k8s",
			Value: fmt.Sprintf("sa:%s", instance.Spec.Selector.ServiceAccount),
		})
	}
	for _, v := range instance.Spec.Selector.Arbitrary {
		selectors = append(selectors, &common.Selector{Value: v})
	}

	spiffeId := instance.Spec.SpiffeId

	regEntryId, err := r.SpireClient.CreateEntry(ctx, &common.RegistrationEntry{
		Selectors: selectors,
		ParentId:  *r.myId,
		SpiffeId:  spiffeId,
		DnsNames:  instance.Spec.DnsNames,
	})
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			entryId, err := r.getExistingEntry(ctx, reqLogger, spiffeId, selectors)
			if err != nil {
				reqLogger.Error(err, "Failed to reuse existing spire entry")
				return "", err
			}
			reqLogger.Info("Found existing entry", "entryID", entryId, "spiffeID", spiffeId)
			return entryId, err
		}
		reqLogger.Error(err, "Failed to create spire entry")
		return "", err
	}
	r.spiffeIDCollection[instance.ObjectMeta.Namespace+"/"+instance.ObjectMeta.Name] = regEntryId.Id
	reqLogger.Info("Created entry", "entryID", regEntryId.Id, "spiffeID", spiffeId)

	return regEntryId.Id, nil

}
