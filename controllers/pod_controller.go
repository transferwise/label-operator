/*
Copyright 2021.

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
	"github.com/go-logr/logr"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch

const (
	podTopologyLabel = "topology.kubernetes.io/zone"
)

// Reconcile handles a reconciliation request for a Pod.
// If the Pod has the addPodNameLabelAnnotation annotation, then Reconcile
// will make sure the podNameLabel label is present with the correct value.
// If the annotation is absent, then Reconcile will make sure the label is too.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("pod", req.NamespacedName)

	/*
		Step 0: Fetch the Pod from the Kubernetes API.
	*/

	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			// we'll ignore not-found errors, since they can't be fixed by an immediate
			// requeue (we'll need to wait for a new notification), and we can get them
			// on deleted requests.
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Pod")
		return ctrl.Result{}, err
	}

	// Logic is as followed:
	// 1. pod state change
	// 2. Find node
	// 3. if pod is being deleted there should be no node
	// 4. do node ops
	// update labels

	node := &corev1.NodeList{}
	if err := r.List(ctx, node); err != nil {
		log.Error(err, "unable to list nodes")
		return ctrl.Result{}, err
	}

	if pod.Spec.NodeName == "" {
		log.Info("Pod being deleted, we don't care about this")
		return ctrl.Result{}, nil
	}

	// Find the node in the list
	idx := slices.IndexFunc(node.Items, func(n corev1.Node) bool { return n.Name == pod.Spec.NodeName })

	// First: check if the topology label exists
	topologyLabelExists := node.Items[idx].Labels[podTopologyLabel] != ""
	if topologyLabelExists {

		// Fetch the topology label from the node
		nodeLabel := node.Items[idx].Labels[podTopologyLabel]

		// Does the label exist on the pod?
		labelIsNotPresent := pod.Labels[podTopologyLabel] == ""

		// if the label does not exist or if the label doesn't match the pod label
		if labelIsNotPresent || pod.Labels[podTopologyLabel] != nodeLabel {

			if pod.Labels == nil {
				pod.Labels = make(map[string]string)
			}

			pod.Labels[podTopologyLabel] = nodeLabel
			log.Info("adding label")

		} else {
			// The desired state and actual state of the Pod are the same.
			// No further action is required by the operator at this moment.
			log.Info("no update required")
			return ctrl.Result{}, nil
		}
	} else {
		log.Info("Topology label does not exist on Node object, skipping.")
		return ctrl.Result{}, nil
	}

	if err := r.Update(ctx, &pod); err != nil {
		if apierrors.IsConflict(err) {
			// The Pod has been updated since we read it.
			// Requeue the Pod to try to reconciliate again.
			return ctrl.Result{Requeue: true}, nil
		}
		if apierrors.IsNotFound(err) {
			// The Pod has been deleted since we read it.
			// Requeue the Pod to try to reconciliate again.
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "unable to update Pod")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}
