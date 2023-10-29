/*
Copyright 2019 The Kubernetes Authors.

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

package common

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"

	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	client "k8s.io/client-go/kubernetes/typed/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var (
	KeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
)

// charMap is a list of string represented values of first 10 integers and lowercase
// alphabets. This is a lookup table to maintain entropy when converting bytes to string.
var charMap []string

// WARNING: PLEASE DO NOT CHANGE THE HASH FUNCTION.
func init() {
	for i := 0; i < 10; i++ {
		charMap = append(charMap, strconv.Itoa(i))
	}
	for i := 0; i < 26; i++ {
		charMap = append(charMap, string('a'+rune(i)))
	}
}

// ContentHash creates a content hash string of length n of s utilizing sha256.
// WARNING: PLEASE DO NOT CHANGE THE HASH FUNCTION.
func ContentHash(s string, n int) string {
	var h string
	bytes := sha256.Sum256(([]byte)(s))
	for i := 0; i < n && i < len(bytes); i++ {
		idx := int(bytes[i]) % len(charMap)
		h += charMap[idx]
	}
	return h
}

// NamespacedName returns namespaced name string of a given ingress.
// Note: This is used for logging.
func NamespacedName(ing *v1.Ingress) string {
	if ing == nil {
		return ""
	}
	return types.NamespacedName{Namespace: ing.Namespace, Name: ing.Name}.String()
}

// IngressKeyFunc returns ingress key for given ingress as generated by Ingress Store.
// This falls back to utility function in case of an error.
// Note: Ingress Store and NamespacedName both return same key in general. But, Ingress Store
// returns <name> where as NamespacedName returns /<name> when <namespace> is empty.
func IngressKeyFunc(ing *v1.Ingress) string {
	ingKey, err := KeyFunc(ing)
	if err == nil {
		return ingKey
	}
	// An error is returned only if ingress object does not have a valid meta.
	// So, this should not happen in production, fall back on utility function to
	// get ingress key.
	klog.Errorf("Cannot get key for Ingress %v/%v: %v, using utility function", ing.Namespace, ing.Name, err)
	return NamespacedName(ing)
}

// ToIngressKeys returns a list of ingress keys for given list of ingresses.
func ToIngressKeys(ings []*v1.Ingress) []string {
	ingKeys := make([]string, 0, len(ings))
	for _, ing := range ings {
		ingKeys = append(ingKeys, IngressKeyFunc(ing))
	}
	return ingKeys
}

// PatchIngressObjectMetadata patches the given ingress's metadata based on new
// ingress metadata.
func PatchIngressObjectMetadata(ic client.IngressInterface, ing *v1.Ingress, newObjectMetadata metav1.ObjectMeta, ingLogger klog.Logger) (*v1.Ingress, error) {
	newIng := ing.DeepCopy()
	newIng.ObjectMeta = newObjectMetadata
	return patchIngress(ic, ing, newIng, ingLogger)
}

// PatchIngressStatus patches the given ingress's Status based on new ingress
// status.
func PatchIngressStatus(ic client.IngressInterface, ing *v1.Ingress, newStatus v1.IngressStatus, ingLogger klog.Logger) (*v1.Ingress, error) {
	newIng := ing.DeepCopy()
	newIng.Status = newStatus
	return patchIngress(ic, ing, newIng, ingLogger)
}

// patchIngress patches the given ingress's Status or ObjectMetadata based on
// the old and new ingresses.
// Note that both Status and ObjectMetadata (annotations and finalizers)
// can be patched via `status` subresource API endpoint.
func patchIngress(ic client.IngressInterface, oldIngress, newIngress *v1.Ingress, ingLogger klog.Logger) (*v1.Ingress, error) {
	ingKey := fmt.Sprintf("%s/%s", oldIngress.Namespace, oldIngress.Name)
	oldData, err := json.Marshal(oldIngress)
	if err != nil {
		return nil, fmt.Errorf("failed to Marshal oldData for ingress %s: %v", ingKey, err)
	}

	newData, err := json.Marshal(newIngress)
	if err != nil {
		return nil, fmt.Errorf("failed to Marshal newData for ingress %s: %v", ingKey, err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1.Ingress{})
	if err != nil {
		return nil, fmt.Errorf("failed to create TwoWayMergePatch for ingress %s: %v", ingKey, err)
	}

	ingLogger.Info("Patch bytes for ingress", "patchBytes", patchBytes)
	return ic.Patch(context.TODO(), oldIngress.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{}, "status")
}
