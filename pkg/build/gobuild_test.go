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

package build

import (
	"archive/tar"
	"context"
	"fmt"
	gb "go/build"
	"io"
	"io/ioutil"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
)

func repoRootDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("could not get current filename")
	}
	basepath := filepath.Dir(filename)
	repoDir := filepath.Join(basepath, "..", "..")
	return filepath.Rel(basepath, repoDir)
}

func TestGoBuildQualifyImport(t *testing.T) {
	base, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}

	repoDir, err := repoRootDir()
	if err != nil {
		t.Fatalf("could not get Git repository root directory")
	}

	tests := []struct {
		description         string
		rawImportpath       string
		dir                 string
		qualifiedImportpath string
		expectError         bool
	}{
		{
			description:         "strict qualified import path",
			rawImportpath:       "ko://github.com/google/ko",
			dir:                 "",
			qualifiedImportpath: "ko://github.com/google/ko",
			expectError:         false,
		},
		{
			description:         "strict qualified import path in subdirectory of go.mod",
			rawImportpath:       "ko://github.com/google/ko/test",
			dir:                 "",
			qualifiedImportpath: "ko://github.com/google/ko/test",
			expectError:         false,
		},
		{
			description:         "non-strict qualified import path",
			rawImportpath:       "github.com/google/ko",
			dir:                 "",
			qualifiedImportpath: "ko://github.com/google/ko",
			expectError:         false,
		},
		{
			description:         "non-strict local import path in repository root directory",
			rawImportpath:       "./test",
			dir:                 repoDir,
			qualifiedImportpath: "ko://github.com/google/ko/test",
			expectError:         false,
		},
		{
			description:         "non-strict local import path in subdirectory",
			rawImportpath:       ".",
			dir:                 filepath.Join(repoDir, "test"),
			qualifiedImportpath: "ko://github.com/google/ko/test",
			expectError:         false,
		},
		{
			description:         "non-existent non-strict local import path",
			rawImportpath:       "./does-not-exist",
			dir:                 "/",
			qualifiedImportpath: "should return error",
			expectError:         true,
		},
	}
	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			ng, err := NewGo(context.Background(), test.dir, WithBaseImages(func(context.Context, string) (Result, error) { return base, nil }))
			if err != nil {
				t.Fatalf("NewGo() = %v", err)
			}
			gotImportpath, err := ng.QualifyImport(test.rawImportpath)
			if err != nil && test.expectError {
				return
			}
			if err != nil && !test.expectError {
				t.Errorf("QualifyImport(dir=%q)(%q) was error (%v), want nil error", test.dir, test.rawImportpath, err)
			}
			if err == nil && test.expectError {
				t.Errorf("QualifyImport(dir=%q)(%q) was nil error, want non-nil error", test.dir, test.rawImportpath)
			}
			if gotImportpath != test.qualifiedImportpath {
				t.Errorf("QualifyImport(dir=%q)(%q) = (%q, nil), want (%q, nil)", test.dir, test.rawImportpath, gotImportpath, test.qualifiedImportpath)
			}
		})
	}
}

func TestGoBuildIsSupportedRef(t *testing.T) {
	base, err := random.Image(1024, 3)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}

	ng, err := NewGo(context.Background(), "", WithBaseImages(func(context.Context, string) (Result, error) { return base, nil }))
	if err != nil {
		t.Fatalf("NewGo() = %v", err)
	}

	// Supported import paths.
	for _, importpath := range []string{
		"ko://github.com/google/ko", // ko can build itself.
	} {
		t.Run(importpath, func(t *testing.T) {
			if err := ng.IsSupportedReference(importpath); err != nil {
				t.Errorf("IsSupportedReference(%q) = (%v), want nil", importpath, err)
			}
		})
	}

	// Unsupported import paths.
	for _, importpath := range []string{
		"ko://github.com/google/ko/pkg/build",       // not a command.
		"ko://github.com/google/ko/pkg/nonexistent", // does not exist.
	} {
		t.Run(importpath, func(t *testing.T) {
			if err := ng.IsSupportedReference(importpath); err == nil {
				t.Errorf("IsSupportedReference(%v) = nil, want error", importpath)
			}
		})
	}
}

func TestGoBuildIsSupportedRefWithModules(t *testing.T) {
	base, err := random.Image(1024, 3)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}

	mods := &modules{
		main: &modInfo{
			Path: "github.com/google/ko/test",
			Dir:  ".",
		},
		deps: map[string]*modInfo{
			"github.com/some/module/cmd": {
				Path: "github.com/some/module/cmd",
				Dir:  ".",
			},
		},
	}

	opts := []Option{
		WithBaseImages(func(context.Context, string) (Result, error) { return base, nil }),
		withModuleInfo(mods),
		withBuildContext(stubBuildContext{
			// make all referenced deps commands
			"github.com/google/ko/test":  &gb.Package{Name: "main"},
			"github.com/some/module/cmd": &gb.Package{Name: "main"},

			"github.com/google/ko/pkg/build": &gb.Package{Name: "build"},
		}),
	}

	ng, err := NewGo(context.Background(), "", opts...)
	if err != nil {
		t.Fatalf("NewGo() = %v", err)
	}

	// Supported import paths.
	for _, importpath := range []string{
		"ko://github.com/google/ko/test",  // ko can build the test package.
		"ko://github.com/some/module/cmd", // ko can build commands in dependent modules
	} {
		t.Run(importpath, func(t *testing.T) {
			if err := ng.IsSupportedReference(importpath); err != nil {
				t.Errorf("IsSupportedReference(%q) = (%v), want nil", err, importpath)
			}
		})
	}

	// Unsupported import paths.
	for _, importpath := range []string{
		"ko://github.com/google/ko/pkg/build",       // not a command.
		"ko://github.com/google/ko/pkg/nonexistent", // does not exist.
		"ko://github.com/google/ko",                 // not in this module.
	} {
		t.Run(importpath, func(t *testing.T) {
			if err := ng.IsSupportedReference(importpath); err == nil {
				t.Errorf("IsSupportedReference(%v) = nil, want error", importpath)
			}
		})
	}
}

// A helper method we use to substitute for the default "build" method.
func writeTempFile(_ context.Context, s string, _ string, _ v1.Platform, _ bool) (string, error) {
	tmpDir, err := ioutil.TempDir("", "ko")
	if err != nil {
		return "", err
	}

	file, err := ioutil.TempFile(tmpDir, "out")
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.WriteString(filepath.ToSlash(s)); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func TestGoBuildNoKoData(t *testing.T) {
	baseLayers := int64(3)
	base, err := random.Image(1024, baseLayers)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}
	importpath := "github.com/google/ko"

	creationTime := v1.Time{Time: time.Unix(5000, 0)}
	ng, err := NewGo(
		context.Background(),
		"",
		WithCreationTime(creationTime),
		WithBaseImages(func(context.Context, string) (Result, error) { return base, nil }),
		withBuilder(writeTempFile),
	)
	if err != nil {
		t.Fatalf("NewGo() = %v", err)
	}

	result, err := ng.Build(context.Background(), StrictScheme+importpath)
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}

	img, ok := result.(v1.Image)
	if !ok {
		t.Fatalf("Build() not an image: %v", result)
	}

	ls, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers() = %v", err)
	}

	// Check that we have the expected number of layers.
	t.Run("check layer count", func(t *testing.T) {
		// We get a layer for the go binary and a layer for the kodata/
		if got, want := int64(len(ls)), baseLayers+2; got != want {
			t.Fatalf("len(Layers()) = %v, want %v", got, want)
		}
	})

	// Check that rebuilding the image again results in the same image digest.
	t.Run("check determinism", func(t *testing.T) {
		result2, err := ng.Build(context.Background(), StrictScheme+importpath)
		if err != nil {
			t.Fatalf("Build() = %v", err)
		}

		d1, err := result.Digest()
		if err != nil {
			t.Fatalf("Digest() = %v", err)
		}
		d2, err := result2.Digest()
		if err != nil {
			t.Fatalf("Digest() = %v", err)
		}

		if d1 != d2 {
			t.Errorf("Digest mismatch: %s != %s", d1, d2)
		}
	})

	// Check that the entrypoint of the image is configured to invoke our Go application
	t.Run("check entrypoint", func(t *testing.T) {
		cfg, err := img.ConfigFile()
		if err != nil {
			t.Errorf("ConfigFile() = %v", err)
		}
		entrypoint := cfg.Config.Entrypoint
		if got, want := len(entrypoint), 1; got != want {
			t.Errorf("len(entrypoint) = %v, want %v", got, want)
		}

		if got, want := entrypoint[0], "/ko-app/ko"; got != want {
			t.Errorf("entrypoint = %v, want %v", got, want)
		}
	})

	t.Run("check creation time", func(t *testing.T) {
		cfg, err := img.ConfigFile()
		if err != nil {
			t.Errorf("ConfigFile() = %v", err)
		}

		actual := cfg.Created
		if actual.Time != creationTime.Time {
			t.Errorf("created = %v, want %v", actual, creationTime)
		}
	})
}

func validateImage(t *testing.T, img v1.Image, baseLayers int64, creationTime v1.Time) {
	t.Helper()

	ls, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers() = %v", err)
	}

	// Check that we have the expected number of layers.
	t.Run("check layer count", func(t *testing.T) {
		// We get a layer for the go binary and a layer for the kodata/
		if got, want := int64(len(ls)), baseLayers+2; got != want {
			t.Fatalf("len(Layers()) = %v, want %v", got, want)
		}
	})

	t.Run("check app layer contents", func(t *testing.T) {
		dataLayer := ls[baseLayers]

		if _, err := dataLayer.Digest(); err != nil {
			t.Errorf("Digest() = %v", err)
		}
		// We don't check the data layer here because it includes a symlink of refs and
		// will produce a distinct hash each time we commit something.

		r, err := dataLayer.Uncompressed()
		if err != nil {
			t.Errorf("Uncompressed() = %v", err)
		}
		defer r.Close()
		tr := tar.NewReader(r)
		if _, err := tr.Next(); err == io.EOF {
			t.Errorf("Layer contained no files")
		}
	})

	// Check that the kodata layer contains the expected data (even though it was a symlink
	// outside kodata).
	t.Run("check kodata", func(t *testing.T) {
		dataLayer := ls[baseLayers]
		r, err := dataLayer.Uncompressed()
		if err != nil {
			t.Errorf("Uncompressed() = %v", err)
		}
		defer r.Close()
		found := false
		tr := tar.NewReader(r)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				t.Errorf("Next() = %v", err)
				continue
			}
			if header.Name != path.Join(kodataRoot, "kenobi") {
				continue
			}
			found = true
			body, err := ioutil.ReadAll(tr)
			if err != nil {
				t.Errorf("ReadAll() = %v", err)
			} else if want, got := "Hello there\n", string(body); got != want {
				t.Errorf("ReadAll() = %v, wanted %v", got, want)
			}
		}
		if !found {
			t.Error("Didn't find expected file in tarball")
		}
	})

	// Check that the entrypoint of the image is configured to invoke our Go application
	t.Run("check entrypoint", func(t *testing.T) {
		cfg, err := img.ConfigFile()
		if err != nil {
			t.Errorf("ConfigFile() = %v", err)
		}
		entrypoint := cfg.Config.Entrypoint
		if got, want := len(entrypoint), 1; got != want {
			t.Errorf("len(entrypoint) = %v, want %v", got, want)
		}

		if got, want := entrypoint[0], "/ko-app/test"; got != want {
			t.Errorf("entrypoint = %v, want %v", got, want)
		}
	})

	// Check that the environment contains the KO_DATA_PATH environment variable.
	t.Run("check KO_DATA_PATH env var", func(t *testing.T) {
		cfg, err := img.ConfigFile()
		if err != nil {
			t.Errorf("ConfigFile() = %v", err)
		}
		found := false
		for _, entry := range cfg.Config.Env {
			if entry == "KO_DATA_PATH="+kodataRoot {
				found = true
			}
		}
		if !found {
			t.Error("Didn't find KO_DATA_PATH.")
		}
	})

	// Check that PATH contains the directory of the produced binary.
	t.Run("check PATH env var", func(t *testing.T) {
		cfg, err := img.ConfigFile()
		if err != nil {
			t.Errorf("ConfigFile() = %v", err)
		}
		found := false
		for _, envVar := range cfg.Config.Env {
			if strings.HasPrefix(envVar, "PATH=") {
				pathValue := strings.TrimPrefix(envVar, "PATH=")
				pathEntries := strings.Split(pathValue, ":")
				for _, pathEntry := range pathEntries {
					if pathEntry == "/ko-app/test" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Error("Didn't find entrypoint in PATH.")
		}
	})

	t.Run("check creation time", func(t *testing.T) {
		cfg, err := img.ConfigFile()
		if err != nil {
			t.Errorf("ConfigFile() = %v", err)
		}

		actual := cfg.Created
		if actual.Time != creationTime.Time {
			t.Errorf("created = %v, want %v", actual, creationTime)
		}
	})
}

type stubBuildContext map[string]*gb.Package

func (s stubBuildContext) Import(path string, srcDir string, mode gb.ImportMode) (*gb.Package, error) {
	p, ok := s[path]
	if ok {
		return p, nil
	}
	return nil, fmt.Errorf("not found: %s", path)
}

func TestGoBuild(t *testing.T) {
	baseLayers := int64(3)
	base, err := random.Image(1024, baseLayers)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}
	importpath := "github.com/google/ko"

	creationTime := v1.Time{Time: time.Unix(5000, 0)}
	ng, err := NewGo(
		context.Background(),
		"",
		WithCreationTime(creationTime),
		WithBaseImages(func(context.Context, string) (Result, error) { return base, nil }),
		withBuilder(writeTempFile),
		WithLabel("foo", "bar"),
		WithLabel("hello", "world"),
	)
	if err != nil {
		t.Fatalf("NewGo() = %v", err)
	}

	result, err := ng.Build(context.Background(), StrictScheme+filepath.Join(importpath, "test"))
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}

	img, ok := result.(v1.Image)
	if !ok {
		t.Fatalf("Build() not an image: %v", result)
	}

	validateImage(t, img, baseLayers, creationTime)

	// Check that rebuilding the image again results in the same image digest.
	t.Run("check determinism", func(t *testing.T) {
		result2, err := ng.Build(context.Background(), StrictScheme+filepath.Join(importpath, "test"))
		if err != nil {
			t.Fatalf("Build() = %v", err)
		}

		d1, err := result.Digest()
		if err != nil {
			t.Fatalf("Digest() = %v", err)
		}
		d2, err := result2.Digest()
		if err != nil {
			t.Fatalf("Digest() = %v", err)
		}

		if d1 != d2 {
			t.Errorf("Digest mismatch: %s != %s", d1, d2)
		}
	})

	t.Run("check labels", func(t *testing.T) {
		cfg, err := img.ConfigFile()
		if err != nil {
			t.Fatalf("ConfigFile() = %v", err)
		}

		want := map[string]string{
			"foo":   "bar",
			"hello": "world",
		}
		got := cfg.Config.Labels
		if d := cmp.Diff(got, want); d != "" {
			t.Fatalf("Labels diff (-got,+want): %s", d)
		}
	})
}

func TestGoBuildIndex(t *testing.T) {
	baseLayers := int64(3)
	images := int64(2)
	base, err := random.Index(1024, baseLayers, images)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}
	importpath := "github.com/google/ko"

	creationTime := v1.Time{Time: time.Unix(5000, 0)}
	ng, err := NewGo(
		context.Background(),
		"",
		WithCreationTime(creationTime),
		WithBaseImages(func(context.Context, string) (Result, error) { return base, nil }),
		WithPlatforms("all"),
		withBuilder(writeTempFile),
	)
	if err != nil {
		t.Fatalf("NewGo() = %v", err)
	}

	result, err := ng.Build(context.Background(), StrictScheme+filepath.Join(importpath, "test"))
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}

	idx, ok := result.(v1.ImageIndex)
	if !ok {
		t.Fatalf("Build() not an image: %v", result)
	}

	im, err := idx.IndexManifest()
	if err != nil {
		t.Fatalf("IndexManifest() = %v", err)
	}

	for _, desc := range im.Manifests {
		img, err := idx.Image(desc.Digest)
		if err != nil {
			t.Fatalf("idx.Image(%s) = %v", desc.Digest, err)
		}
		validateImage(t, img, baseLayers, creationTime)
	}

	if want, got := images, int64(len(im.Manifests)); want != got {
		t.Fatalf("len(Manifests()) = %v, want %v", got, want)
	}

	// Check that rebuilding the image again results in the same image digest.
	t.Run("check determinism", func(t *testing.T) {
		result2, err := ng.Build(context.Background(), StrictScheme+filepath.Join(importpath, "test"))
		if err != nil {
			t.Fatalf("Build() = %v", err)
		}

		d1, err := result.Digest()
		if err != nil {
			t.Fatalf("Digest() = %v", err)
		}
		d2, err := result2.Digest()
		if err != nil {
			t.Fatalf("Digest() = %v", err)
		}

		if d1 != d2 {
			t.Errorf("Digest mismatch: %s != %s", d1, d2)
		}
	})
}

func TestNestedIndex(t *testing.T) {
	baseLayers := int64(3)
	images := int64(2)
	base, err := random.Index(1024, baseLayers, images)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}
	importpath := "github.com/google/ko"

	nestedBase := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: base})

	creationTime := v1.Time{Time: time.Unix(5000, 0)}
	ng, err := NewGo(
		context.Background(),
		"",
		WithCreationTime(creationTime),
		WithBaseImages(func(context.Context, string) (Result, error) { return nestedBase, nil }),
		withBuilder(writeTempFile),
	)
	if err != nil {
		t.Fatalf("NewGo() = %v", err)
	}

	_, err = ng.Build(context.Background(), StrictScheme+filepath.Join(importpath, "test"))
	if err == nil {
		t.Fatal("Build() expected err")
	}

	if !strings.Contains(err.Error(), "unexpected mediaType") {
		t.Errorf("Build() expected unexpected mediaType error, got: %s", err)
	}
}

func TestGoarm(t *testing.T) {
	// From golang@sha256:1ba0da74b20aad52b091877b0e0ece503c563f39e37aa6b0e46777c4d820a2ae
	// and made up invalid cases.
	for _, tc := range []struct {
		platform v1.Platform
		variant  string
		err      bool
	}{{
		platform: v1.Platform{
			Architecture: "arm",
			OS:           "linux",
			Variant:      "vnot-a-number",
		},
		err: true,
	}, {
		platform: v1.Platform{
			Architecture: "arm",
			OS:           "linux",
			Variant:      "wrong-prefix",
		},
		err: true,
	}, {
		platform: v1.Platform{
			Architecture: "arm64",
			OS:           "linux",
			Variant:      "v3",
		},
		variant: "",
	}, {
		platform: v1.Platform{
			Architecture: "arm",
			OS:           "linux",
			Variant:      "v5",
		},
		variant: "5",
	}, {
		platform: v1.Platform{
			Architecture: "arm",
			OS:           "linux",
			Variant:      "v7",
		},
		variant: "7",
	}, {
		platform: v1.Platform{
			Architecture: "arm64",
			OS:           "linux",
			Variant:      "v8",
		},
		variant: "7",
	},
	} {
		variant, err := getGoarm(tc.platform)
		if tc.err {
			if err == nil {
				t.Errorf("getGoarm(%v) expected err", tc.platform)
			}
			continue
		}
		if err != nil {
			t.Fatalf("getGoarm failed for %v: %v", tc.platform, err)
		}
		if got, want := variant, tc.variant; got != want {
			t.Errorf("wrong variant for %v: want %q got %q", tc.platform, want, got)
		}
	}
}

func TestMatchesPlatformSpec(t *testing.T) {
	for _, tc := range []struct {
		platform *v1.Platform
		spec     string
		result   bool
		err      bool
	}{{
		platform: nil,
		spec:     "all",
		result:   true,
	}, {
		platform: nil,
		spec:     "linux/amd64",
		result:   false,
	}, {
		platform: &v1.Platform{
			Architecture: "amd64",
			OS:           "linux",
		},
		spec:   "all",
		result: true,
	}, {
		platform: &v1.Platform{
			Architecture: "amd64",
			OS:           "windows",
		},
		spec:   "linux",
		result: false,
	}, {
		platform: &v1.Platform{
			Architecture: "arm64",
			OS:           "linux",
			Variant:      "v3",
		},
		spec:   "linux/amd64,linux/arm64",
		result: true,
	}, {
		platform: &v1.Platform{
			Architecture: "arm64",
			OS:           "linux",
			Variant:      "v3",
		},
		spec:   "linux/amd64,linux/arm64/v4",
		result: false,
	}, {
		platform: &v1.Platform{
			Architecture: "arm64",
			OS:           "linux",
			Variant:      "v3",
		},
		spec: "linux/amd64,linux/arm64/v3/z5",
		err:  true,
	}, {
		spec: "",
		platform: &v1.Platform{
			Architecture: "amd64",
			OS:           "linux",
		},
		result: false,
	}} {
		pm, err := parseSpec(tc.spec)
		if tc.err {
			if err == nil {
				t.Errorf("parseSpec(%v, %q) expected err", tc.platform, tc.spec)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseSpec failed for %v %q: %v", tc.platform, tc.spec, err)
		}
		matches := pm.matches(tc.platform)
		if got, want := matches, tc.result; got != want {
			t.Errorf("wrong result for %v %q: want %t got %t", tc.platform, tc.spec, want, got)
		}
	}
}
