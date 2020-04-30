// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package options

import (
	"crypto/md5"
	"encoding/hex"
	"path/filepath"

	"github.com/google/ko/pkg/parameters"
	"github.com/google/ko/pkg/publish"
	"github.com/spf13/cobra"
)

func AddPublishArg(cmd *cobra.Command, po *parameters.PublishParameters) {
	cmd.Flags().StringSliceVarP(&po.Tags, "tags", "t", []string{"latest"},
		"Which tags to use for the produced image instead of the default 'latest' tag.")

	cmd.Flags().BoolVar(&po.Push, "push", true, "Push images to KO_DOCKER_REPO")

	cmd.Flags().BoolVarP(&po.Local, "local", "L", po.Local,
		"Load into images to local docker daemon.")
	cmd.Flags().BoolVar(&po.InsecureRegistry, "insecure-registry", po.InsecureRegistry,
		"Whether to skip TLS verification on the registry")

	cmd.Flags().StringVar(&po.OCILayoutPath, "oci-layout-path", "", "Path to save the OCI image layout of the built images")
	cmd.Flags().StringVar(&po.TarballFile, "tarball", "", "File to save images tarballs")

	cmd.Flags().BoolVarP(&po.PreserveImportPaths, "preserve-import-paths", "P", po.PreserveImportPaths,
		"Whether to preserve the full import path after KO_DOCKER_REPO.")
	cmd.Flags().BoolVarP(&po.BaseImportPaths, "base-import-paths", "B", po.BaseImportPaths,
		"Whether to use the base path without MD5 hash after KO_DOCKER_REPO.")
}

func packageWithMD5(importpath string) string {
	hasher := md5.New()
	hasher.Write([]byte(importpath))
	return filepath.Base(importpath) + "-" + hex.EncodeToString(hasher.Sum(nil))
}

func preserveImportPath(importpath string) string {
	return importpath
}

func baseImportPaths(importpath string) string {
	return filepath.Base(importpath)
}

func MakeNamer(po *parameters.PublishParameters) publish.Namer {
	if po.PreserveImportPaths {
		return preserveImportPath
	} else if po.BaseImportPaths {
		return baseImportPaths
	}
	return packageWithMD5
}
