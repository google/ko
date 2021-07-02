/*
Copyright 2018 Google LLC All Rights Reserved.

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

package publish

import (
	"context"
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/random"
)

type mockClient struct {
	daemon.Client
}

func (m *mockClient) NegotiateAPIVersion(context.Context) {}
func (m *mockClient) ImageLoad(context.Context, io.Reader, bool) (types.ImageLoadResponse, error) {
	return types.ImageLoadResponse{
		Body: ioutil.NopCloser(strings.NewReader("Loaded")),
	}, nil
}

func (m *mockClient) ImageTag(_ context.Context, _ string, tag string) error {
	Tags = append(Tags, tag)
	return nil
}

var Tags []string

func TestDaemon(t *testing.T) {
	importpath := "github.com/google/ko"
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}

	def, err := NewDaemon(md5Hash, []string{}, WithDockerClient(&mockClient{}))
	if err != nil {
		t.Fatalf("NewDaemon() = %v", err)
	}

	if d, err := def.Publish(context.Background(), img, importpath); err != nil {
		t.Errorf("Publish() = %v", err)
	} else if got, want := d.String(), md5Hash("ko.local", importpath); !strings.HasPrefix(got, want) {
		t.Errorf("Publish() = %v, wanted prefix %v", got, want)
	}
}

func TestDaemonTags(t *testing.T) {
	Tags = nil

	importpath := "github.com/google/ko"
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}

	def, err := NewDaemon(md5Hash, []string{"v2.0.0", "v1.2.3", "production"}, WithDockerClient(&mockClient{}))
	if err != nil {
		t.Fatalf("NewDaemon() = %v", err)
	}

	if d, err := def.Publish(context.Background(), img, importpath); err != nil {
		t.Errorf("Publish() = %v", err)
	} else if got, want := d.String(), md5Hash("ko.local", importpath); !strings.HasPrefix(got, want) {
		t.Errorf("Publish() = %v, wanted prefix %v", got, want)
	}

	expected := []string{"ko.local/98b8c7facdad74510a7cae0cd368eb4e:v2.0.0", "ko.local/98b8c7facdad74510a7cae0cd368eb4e:v1.2.3", "ko.local/98b8c7facdad74510a7cae0cd368eb4e:production"}

	for i, v := range expected {
		if Tags[i] != v {
			t.Errorf("Expected tag %v got %v", v, Tags[i])
		}
	}
}

func TestDaemonDomain(t *testing.T) {
	Tags = nil

	importpath := "github.com/google/ko"
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}

	localDomain := "registry.example.com/repository"
	def, err := NewDaemon(md5Hash, []string{}, WithLocalDomain(localDomain), WithDockerClient(&mockClient{}))
	if err != nil {
		t.Fatalf("NewDaemon() = %v", err)
	}

	if d, err := def.Publish(context.Background(), img, importpath); err != nil {
		t.Errorf("Publish() = %v", err)
	} else if got, want := d.String(), md5Hash(localDomain, importpath); !strings.HasPrefix(got, want) {
		t.Errorf("Publish() = %v, wanted prefix %v", got, want)
	}
}
