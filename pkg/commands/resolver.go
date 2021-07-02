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

package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/ko/pkg/build"
	"github.com/google/ko/pkg/commands/options"
	"github.com/google/ko/pkg/publish"
	"github.com/google/ko/pkg/resolve"
	"github.com/mattmoor/dep-notify/pkg/graph"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/labels"
)

// ua returns the ko user agent.
func ua() string {
	if v := version(); v != "" {
		return "ko/" + v
	}
	return "ko"
}

func gobuildOptions(bo *options.BuildOptions) ([]build.Option, error) {
	creationTime, err := getCreationTime()
	if err != nil {
		return nil, err
	}

	kodataCreationTime, err := getKoDataCreationTime()
	if err != nil {
		return nil, err
	}

	platform := bo.Platform
	if platform == "" {
		platform = "linux/amd64"

		goos, goarch, goarm := os.Getenv("GOOS"), os.Getenv("GOARCH"), os.Getenv("GOARM")

		// Default to linux/amd64 unless GOOS and GOARCH are set.
		if goos != "" && goarch != "" {
			platform = path.Join(goos, goarch)
		}

		// Use GOARM for variant if it's set and GOARCH is arm.
		if strings.Contains(goarch, "arm") && goarm != "" {
			platform = path.Join(platform, "v"+goarm)
		}
	} else {
		// Make sure these are all unset
		for _, env := range []string{"GOOS", "GOARCH", "GOARM"} {
			if s, ok := os.LookupEnv(env); ok {
				return nil, fmt.Errorf("cannot use --platform with %s=%q", env, s)
			}
		}
	}

	opts := []build.Option{
		build.WithBaseImages(getBaseImage(platform, bo)),
		build.WithPlatforms(platform),
	}
	if creationTime != nil {
		opts = append(opts, build.WithCreationTime(*creationTime))
	}
	if kodataCreationTime != nil {
		opts = append(opts, build.WithKoDataCreationTime(*kodataCreationTime))
	}
	if bo.DisableOptimizations {
		opts = append(opts, build.WithDisabledOptimizations())
	}
	for _, lf := range bo.Labels {
		parts := strings.SplitN(lf, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid label flag: %s", lf)
		}
		opts = append(opts, build.WithLabel(parts[0], parts[1]))
	}

	if len(buildConfigs) > 0 {
		opts = append(opts, build.WithConfig(buildConfigs))
	}

	return opts, nil
}

// NewBuilder creates a ko builder
func NewBuilder(ctx context.Context, bo *options.BuildOptions) (build.Interface, error) {
	return makeBuilder(ctx, bo)
}

func makeBuilder(ctx context.Context, bo *options.BuildOptions) (*build.Caching, error) {
	opt, err := gobuildOptions(bo)
	if err != nil {
		return nil, fmt.Errorf("error setting up builder options: %v", err)
	}
	innerBuilder, err := build.NewGo(ctx, bo.WorkingDirectory, opt...)
	if err != nil {
		return nil, err
	}

	innerBuilder = build.NewLimiter(innerBuilder, bo.ConcurrentBuilds)

	// tl;dr Wrap builder in a caching builder.
	//
	// The caching builder should on Build calls:
	//  - Check for a valid Build future
	//    - if a valid Build future exists at the time of the request,
	//      then block on it.
	//    - if it does not, then initiate and record a Build future.
	//  - When import paths are "affected" by filesystem changes during a
	//    Watch, then invalidate their build futures *before* we put the
	//    affected yaml files onto the channel
	//
	// This will benefit the following key cases:
	// 1. When the same import path is referenced across multiple yaml files
	//    we can elide subsequent builds by blocking on the same image future.
	// 2. When an affected yaml file has multiple import paths (mostly unaffected)
	//    we can elide the builds of unchanged import paths.
	return build.NewCaching(innerBuilder)
}

// NewPublisher creates a ko publisher
func NewPublisher(po *options.PublishOptions) (publish.Interface, error) {
	return makePublisher(po)
}

func makePublisher(po *options.PublishOptions) (publish.Interface, error) {
	// Create the publish.Interface that we will use to publish image references
	// to either a docker daemon or a container image registry.
	innerPublisher, err := func() (publish.Interface, error) {
		repoName := po.DockerRepo
		namer := options.MakeNamer(po)
		if repoName == publish.LocalDomain || po.Local {
			// TODO(jonjohnsonjr): I'm assuming that nobody will
			// use local with other publishers, but that might
			// not be true.
			return publish.NewDaemon(namer, po.Tags,
				publish.WithDockerClient(po.DockerClient),
				publish.WithLocalDomain(po.LocalDomain),
			)
		}
		if repoName == publish.KindDomain {
			return publish.NewKindPublisher(namer, po.Tags), nil
		}

		if repoName == "" {
			return nil, errors.New("KO_DOCKER_REPO environment variable is unset")
		}
		if _, err := name.NewRegistry(repoName); err != nil {
			if _, err := name.NewRepository(repoName); err != nil {
				return nil, fmt.Errorf("failed to parse %q as repository: %v", repoName, err)
			}
		}

		publishers := []publish.Interface{}
		if po.OCILayoutPath != "" {
			lp, err := publish.NewLayout(po.OCILayoutPath)
			if err != nil {
				return nil, fmt.Errorf("failed to create LayoutPublisher for %q: %v", po.OCILayoutPath, err)
			}
			publishers = append(publishers, lp)
		}
		if po.TarballFile != "" {
			tp := publish.NewTarball(po.TarballFile, repoName, namer, po.Tags)
			publishers = append(publishers, tp)
		}
		userAgent := ua()
		if po.UserAgent != "" {
			userAgent = po.UserAgent
		}
		if po.Push {
			dp, err := publish.NewDefault(repoName,
				publish.WithUserAgent(userAgent),
				publish.WithAuthFromKeychain(authn.DefaultKeychain),
				publish.WithNamer(namer),
				publish.WithTags(po.Tags),
				publish.WithTagOnly(po.TagOnly),
				publish.Insecure(po.InsecureRegistry))
			if err != nil {
				return nil, err
			}
			publishers = append(publishers, dp)
		}

		// If not publishing, at least generate a digest to simulate
		// publishing.
		if len(publishers) == 0 {
			publishers = append(publishers, nopPublisher{
				repoName: repoName,
				namer:    namer,
			})
		}

		return publish.MultiPublisher(publishers...), nil
	}()
	if err != nil {
		return nil, err
	}

	// Wrap publisher in a memoizing publisher implementation.
	return publish.NewCaching(innerPublisher)
}

// nopPublisher simulates publishing without actually publishing anything, to
// provide fallback behavior when the user configures no push destinations.
type nopPublisher struct {
	repoName string
	namer    publish.Namer
}

func (n nopPublisher) Publish(_ context.Context, br build.Result, s string) (name.Reference, error) {
	s = strings.TrimPrefix(s, build.StrictScheme)
	h, err := br.Digest()
	if err != nil {
		return nil, err
	}
	return name.NewDigest(fmt.Sprintf("%s@%s", n.namer(n.repoName, s), h))
}

func (n nopPublisher) Close() error { return nil }

// resolvedFuture represents a "future" for the bytes of a resolved file.
type resolvedFuture chan []byte

func resolveFilesToWriter(
	ctx context.Context,
	builder *build.Caching,
	publisher publish.Interface,
	fo *options.FilenameOptions,
	so *options.SelectorOptions,
	out io.WriteCloser) error {
	defer out.Close()

	// By having this as a channel, we can hook this up to a filesystem
	// watcher and leave `fs` open to stream the names of yaml files
	// affected by code changes (including the modification of existing or
	// creation of new yaml files).
	fs := options.EnumerateFiles(fo)

	// This tracks filename -> []importpath
	var sm sync.Map

	var g graph.Interface
	var errCh chan error
	var err error
	if fo.Watch {
		// Start a dep-notify process that on notifications scans the
		// file-to-recorded-build map and for each affected file resends
		// the filename along the channel.
		g, errCh, err = graph.New(func(ss graph.StringSet) {
			sm.Range(func(k, v interface{}) bool {
				key := k.(string)
				value := v.([]string)

				for _, ip := range value {
					// dep-notify doesn't understand the ko:// prefix
					ip := strings.TrimPrefix(ip, build.StrictScheme)
					if ss.Has(ip) {
						// See the comment above about how "builder" works.
						// Always use ko:// for the builder.
						builder.Invalidate(build.StrictScheme + ip)
						fs <- key
					}
				}
				return true
			})
		})
		if err != nil {
			return fmt.Errorf("creating dep-notify graph: %v", err)
		}
		// Cleanup the fsnotify hooks when we're done.
		defer g.Shutdown()
	}

	// This tracks resolution errors and ensures we cancel other builds if an
	// individual build fails.
	errs, ctx := errgroup.WithContext(ctx)

	var futures []resolvedFuture
	for {
		// Each iteration, if there is anything in the list of futures,
		// listen to it in addition to the file enumerating channel.
		// A nil channel is never available to receive on, so if nothing
		// is available, this will result in us exclusively selecting
		// on the file enumerating channel.
		var bf resolvedFuture
		if len(futures) > 0 {
			bf = futures[0]
		} else if fs == nil {
			// There are no more files to enumerate and the futures
			// have been drained, so quit.
			break
		}

		select {
		case file, ok := <-fs:
			if !ok {
				// a nil channel is never available to receive on.
				// This allows us to drain the list of in-process
				// futures without this case of the select winning
				// each time.
				fs = nil
				break
			}

			// Make a new future to use to ship the bytes back and append
			// it to the list of futures (see comment below about ordering).
			ch := make(resolvedFuture)
			futures = append(futures, ch)

			// Kick off the resolution that will respond with its bytes on
			// the future.
			f := file // defensive copy
			errs.Go(func() error {
				defer close(ch)
				// Record the builds we do via this builder.
				recordingBuilder := &build.Recorder{
					Builder: builder,
				}
				b, err := resolveFile(ctx, f, recordingBuilder, publisher, so)
				if err != nil {
					// This error is sometimes expected during watch mode, so this
					// isn't fatal. Just print it and keep the watch open.
					err := fmt.Errorf("error processing import paths in %q: %v", f, err)
					if fo.Watch {
						log.Print(err)
						return nil
					}
					return err
				}
				// Associate with this file the collection of binary import paths.
				sm.Store(f, recordingBuilder.ImportPaths)
				ch <- b
				if fo.Watch {
					for _, ip := range recordingBuilder.ImportPaths {
						// dep-notify doesn't understand the ko:// prefix
						ip := strings.TrimPrefix(ip, build.StrictScheme)

						// Technically we never remove binary targets from the graph,
						// which will increase our graph's watch load, but the
						// notifications that they change will result in no affected
						// yamls, and no new builds or deploys.
						if err := g.Add(ip); err != nil {
							// If we're in watch mode, just fail.
							err := fmt.Errorf("adding importpath %q to dep graph: %v", ip, err)
							errCh <- err
							return err
						}
					}
				}
				return nil
			})

		case b, ok := <-bf:
			// Once the head channel returns something, dequeue it.
			// We listen to the futures in order to be respectful of
			// the kubectl apply ordering, which matters!
			futures = futures[1:]
			if ok {
				// Write the next body and a trailing delimiter.
				// We write the delimeter LAST so that when streamed to
				// kubectl it knows that the resource is complete and may
				// be applied.
				out.Write(append(b, []byte("\n---\n")...))
			}

		case err := <-errCh:
			return fmt.Errorf("watching dependencies: %v", err)
		}
	}

	// Make sure we exit with an error.
	// See https://github.com/google/ko/issues/84
	return errs.Wait()
}

func resolveFile(
	ctx context.Context,
	f string,
	builder build.Interface,
	pub publish.Interface,
	so *options.SelectorOptions) (b []byte, err error) {

	var selector labels.Selector
	if so.Selector != "" {
		var err error
		selector, err = labels.Parse(so.Selector)

		if err != nil {
			return nil, fmt.Errorf("unable to parse selector: %v", err)
		}
	}

	if f == "-" {
		b, err = ioutil.ReadAll(os.Stdin)
	} else {
		b, err = ioutil.ReadFile(f)
	}
	if err != nil {
		return nil, err
	}

	var docNodes []*yaml.Node

	// The loop is to support multi-document yaml files.
	// This is handled by using a yaml.Decoder and reading objects until io.EOF, see:
	// https://godoc.org/gopkg.in/yaml.v3#Decoder.Decode
	decoder := yaml.NewDecoder(bytes.NewBuffer(b))
	for {
		var doc yaml.Node
		if err := decoder.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		if selector != nil {
			if match, err := resolve.MatchesSelector(&doc, selector); err != nil {
				return nil, fmt.Errorf("error evaluating selector: %v", err)
			} else if !match {
				continue
			}
		}

		docNodes = append(docNodes, &doc)

	}

	if err := resolve.ImageReferences(ctx, docNodes, builder, pub); err != nil {
		return nil, fmt.Errorf("error resolving image references: %v", err)
	}

	buf := &bytes.Buffer{}
	e := yaml.NewEncoder(buf)
	e.SetIndent(2)

	for _, doc := range docNodes {
		err := e.Encode(doc)
		if err != nil {
			return nil, fmt.Errorf("failed to encode output: %v", err)
		}
	}
	e.Close()

	return buf.Bytes(), nil
}
