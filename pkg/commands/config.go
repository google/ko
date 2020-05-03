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

package commands

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/spf13/viper"
)

var (
	defaultBaseImage   name.Reference
	baseImageOverrides map[string]name.Reference
)

func getBaseImage(s string) (v1.Image, error) {
	// Viper configuration file keys are case insensitive, and are
	// returned as all lowercase.  This means that import paths with
	// uppercase must be normalized for matching here, e.g.
	//    github.com/GoogleCloudPlatform/foo/cmd/bar
	// comes through as:
	//    github.com/googlecloudplatform/foo/cmd/bar
	ref, ok := baseImageOverrides[strings.ToLower(s)]
	if !ok {
		ref = defaultBaseImage
	}
	log.Printf("Using base %s for %s", ref, s)
	return remote.Image(ref,
		remote.WithTransport(defaultTransport()),
		remote.WithAuthFromKeychain(authn.DefaultKeychain))
}

func getCreationTime() (*v1.Time, error) {
	epoch := os.Getenv("SOURCE_DATE_EPOCH")
	if epoch == "" {
		return nil, nil
	}

	seconds, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("the environment variable SOURCE_DATE_EPOCH should be the number of seconds since January 1st 1970, 00:00 UTC, got: %v", err)
	}
	return &v1.Time{time.Unix(seconds, 0)}, nil
}

func createCancellableContext() context.Context {
	signals := make(chan os.Signal)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-signals
		cancel()
	}()

	return ctx
}

const Deprecation160 = `NOTICE!
-----------------------------------------------------------------
We are changing the default base image in a subsequent release.

For more information (including how to suppress this message):

   https://github.com/google/ko/issues/160

-----------------------------------------------------------------
`

func init() {
	// If omitted, use this base image.
	viper.SetConfigName(".ko") // .yaml is implicit
	viper.SetEnvPrefix("KO")
	viper.AutomaticEnv()

	if override := os.Getenv("KO_CONFIG_PATH"); override != "" {
		viper.AddConfigPath(override)
	}

	viper.AddConfigPath("./")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			log.Fatalf("error reading config file: %v", err)
		}
	}

	if !viper.IsSet("defaultBaseImage") {
		viper.Set("defaultBaseImage", "gcr.io/distroless/static:latest")
		log.Print(Deprecation160)
	}

	ref := viper.GetString("defaultBaseImage")
	dbi, err := name.ParseReference(ref)
	if err != nil {
		log.Fatalf("'defaultBaseImage': error parsing %q as image reference: %v", ref, err)
	}
	defaultBaseImage = dbi

	baseImageOverrides = make(map[string]name.Reference)
	overrides := viper.GetStringMapString("baseImageOverrides")
	for k, v := range overrides {
		bi, err := name.ParseReference(v)
		if err != nil {
			log.Fatalf("'baseImageOverrides': error parsing %q as image reference: %v", v, err)
		}
		baseImageOverrides[k] = bi
	}
}
