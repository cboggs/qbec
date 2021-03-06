/*
   Copyright 2019 Splunk Inc.

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

package remote

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/googleapis/gnostic/OpenAPIv2"
	"github.com/splunk/qbec/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type disco struct {
	Groups        *metav1.APIGroupList               `json:"groups"`
	ResourceLists map[string]*metav1.APIResourceList `json:"resourceLists"`
}

func (d *disco) ServerGroups() (*metav1.APIGroupList, error) {
	return d.Groups, nil
}

func (d *disco) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	parts := strings.SplitN(groupVersion, "/", 2)
	var group, version string
	if len(parts) == 2 {
		group, version = parts[0], parts[1]
	} else {
		version = parts[0]
	}
	key := fmt.Sprintf("%s:%s", group, version)
	rl := d.ResourceLists[key]
	if rl == nil {
		return nil, fmt.Errorf("no resources for %s", groupVersion)
	}
	return rl, nil
}

func (d *disco) OpenAPISchema() (*openapi_v2.Document, error) {
	b, err := ioutil.ReadFile(filepath.Join("testdata", "swagger-2.0.0.pb-v1"))
	if err != nil {
		return nil, err
	}
	var doc openapi_v2.Document
	if err := proto.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func getServerMetadata(t *testing.T, verbosity int) *ServerMetadata {
	var d disco
	b, err := ioutil.ReadFile(filepath.Join("testdata", "metadata.json"))
	require.Nil(t, err)
	err = json.Unmarshal(b, &d)
	require.Nil(t, err)
	sm, err := newServerMetadata(&d, "foobar", verbosity)
	require.Nil(t, err)
	return sm
}

func loadObject(t *testing.T, file string) model.K8sObject {
	b, err := ioutil.ReadFile(filepath.Join("testdata", file))
	require.Nil(t, err)
	var d map[string]interface{}
	err = json.Unmarshal(b, &d)
	require.Nil(t, err)
	return model.NewK8sObject(d)
}

func TestMetadataCanonical(t *testing.T) {
	a := assert.New(t)
	sm := getServerMetadata(t, 2)
	expected := schema.GroupVersionKind{Group: "extensions", Version: "v1beta1", Kind: "Deployment"}

	canon, err := sm.canonicalGroupVersionKind(expected)
	require.Nil(t, err)
	a.EqualValues(expected, canon)

	canon, err = sm.canonicalGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	require.Nil(t, err)
	a.EqualValues(expected, canon)

	canon, err = sm.canonicalGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1beta1", Kind: "Deployment"})
	require.Nil(t, err)
	a.EqualValues(expected, canon)
}

func TestMetadataOther(t *testing.T) {
	a := assert.New(t)
	sm := getServerMetadata(t, 0)
	n, err := sm.IsNamespaced(schema.GroupVersionKind{Group: "extensions", Version: "v1beta1", Kind: "Deployment"})
	require.Nil(t, err)
	a.True(n)

	n, err = sm.IsNamespaced(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"})
	require.Nil(t, err)
	a.False(n)

	_, err = sm.IsNamespaced(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "FooBar"})
	require.NotNil(t, err)
	a.Equal("server does not recognize gvk /v1, Kind=FooBar", err.Error())

	name := sm.DisplayName(loadObject(t, "ns-good.json"))
	a.Equal("namespaces foobar", name)

	ob := loadObject(t, "ns-good.json")
	name = sm.DisplayName(model.NewK8sLocalObject(ob.ToUnstructured().Object, "app1", "c1", "dev"))
	a.Equal("namespaces foobar (source c1)", name)
}

func TestMetadataValidator(t *testing.T) {
	a := assert.New(t)
	sm := getServerMetadata(t, 0)
	v, err := sm.ValidatorFor(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"})
	require.Nil(t, err)
	errs := v.Validate(loadObject(t, "ns-good.json").ToUnstructured())
	require.Nil(t, errs)

	errs = v.Validate(loadObject(t, "ns-bad.json").ToUnstructured())
	require.NotNil(t, errs)
	a.Equal(1, len(errs))
	a.Contains(errs[0].Error(), `unknown field "foo"`)

	_, err = sm.ValidatorFor(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "FooBar"})
	require.NotNil(t, err)
	a.Equal(ErrSchemaNotFound, err)

}
