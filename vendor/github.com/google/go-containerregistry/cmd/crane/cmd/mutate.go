// Copyright 2021 Google LLC All Rights Reserved.
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

package cmd

import (
	"fmt"
	"log"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/spf13/cobra"
)

// NewCmdMutate creates a new cobra.Command for the mutate subcommand.
func NewCmdMutate(options *[]crane.Option) *cobra.Command {
	var lbls []string
	var entrypoint string
	var newRef string
	var anntns []string

	mutateCmd := &cobra.Command{
		Use:   "mutate",
		Short: "Modify image labels and annotations",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			// Pull image and get config.
			ref := args[0]

			if len(anntns) != 0 {
				desc, err := crane.Head(ref, *options...)
				if err != nil {
					log.Fatalf("checking %s: %v", ref, err)
				}
				if desc.MediaType.IsIndex() {
					log.Fatalf("mutating annotations on an index is not yet supported")
				}
			}

			img, err := crane.Pull(ref, *options...)
			if err != nil {
				log.Fatalf("pulling %s: %v", ref, err)
			}
			cfg, err := img.ConfigFile()
			if err != nil {
				log.Fatalf("getting config: %v", err)
			}
			cfg = cfg.DeepCopy()

			// Set labels.
			if cfg.Config.Labels == nil {
				cfg.Config.Labels = map[string]string{}
			}

			labels, err := splitKeyVals(lbls)
			if err != nil {
				log.Fatal(err)
			}

			for k, v := range labels {
				cfg.Config.Labels[k] = v
			}

			annotations, err := splitKeyVals(anntns)
			if err != nil {
				log.Fatal(err)
			}

			// Set entrypoint.
			if entrypoint != "" {
				// NB: This doesn't attempt to do anything smart about splitting the string into multiple entrypoint elements.
				cfg.Config.Entrypoint = []string{entrypoint}
			}

			// Mutate and write image.
			img, err = mutate.Config(img, cfg.Config)
			if err != nil {
				log.Fatalf("mutating config: %v", err)
			}

			// Mutate and write image.
			img, err = mutate.Annotations(img, annotations)
			if err != nil {
				log.Fatalf("mutating annotations: %v", err)
			}

			// If the new ref isn't provided, write over the original image.
			// If that ref was provided by digest (e.g., output from
			// another crane command), then strip that and push the
			// mutated image by digest instead.
			if newRef == "" {
				newRef = ref
			}
			digest, err := img.Digest()
			if err != nil {
				log.Fatalf("digesting new image: %v", err)
			}
			r, err := name.ParseReference(newRef)
			if err != nil {
				log.Fatalf("parsing %s: %v", newRef, err)
			}
			if _, ok := r.(name.Digest); ok {
				newRef = r.Context().Digest(digest.String()).String()
			}
			if err := crane.Push(img, newRef, *options...); err != nil {
				log.Fatalf("pushing %s: %v", newRef, err)
			}
			fmt.Println(r.Context().Digest(digest.String()))
		},
	}
	mutateCmd.Flags().StringSliceVarP(&anntns, "annotation", "a", nil, "New annotations to add")
	mutateCmd.Flags().StringSliceVarP(&lbls, "label", "l", nil, "New labels to add")
	mutateCmd.Flags().StringVar(&entrypoint, "entrypoint", "", "New entrypoing to set")
	mutateCmd.Flags().StringVarP(&newRef, "tag", "t", "", "New tag to apply to mutated image. If not provided, push by digest to the original image repository.")
	return mutateCmd
}

// splitKeyVals splits key value pairs which is in form hello=world
func splitKeyVals(kvPairs []string) (map[string]string, error) {
	m := map[string]string{}
	for _, l := range kvPairs {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) == 1 {
			return nil, fmt.Errorf("parsing label %q, not enough parts", l)
		}
		m[parts[0]] = parts[1]
	}
	return m, nil
}
