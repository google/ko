# `ko`: Easy Go Containers

[![Travis Build Status](https://travis-ci.org/google/ko.svg?branch=main)](https://travis-ci.org/google/ko)
[![GitHub Actions Build Status](https://github.com/google/ko/workflows/Build/badge.svg)](https://github.com/google/ko/actions?query=workflow%3ABuild)
[![GoDoc](https://godoc.org/github.com/google/ko?status.svg)](https://godoc.org/github.com/google/ko)
[![Go Report Card](https://goreportcard.com/badge/google/ko)](https://goreportcard.com/report/google/ko)

<img src="./logo/ko.png" width="300">

`ko` is a simple, fast container image builder for Go applications.

It's ideal for use cases where your image contains a single Go application
without any/many dependencies on the OS base image (e.g., no cgo, no OS package
dependencies).

`ko` builds images by effectively executing `go build` on your local machine,
and as such doesn't require `docker` to be installed. This can make it a good
fit for lightweight CI/CD use cases.

`ko` also includes support for simple YAML templating which makes it a powerful
tool for Kubernetes applications ([See below](#Kubernetes-Integration)).

# Setup

## Install

### Install from [Releases](https://github.com/google/ko/releases)

```
VERSION=TODO # choose the latest version
OS=Linux     # or Darwin
ARCH=x86_64  # or arm64, i386, s390x
curl -L https://github.com/google/ko/releases/download/v${VERSION}/ko_${VERSION}_${OS}_${ARCH}.tar.gz | tar xzf - ko
chmod +x ./ko
```

### Install using [Homebrew](https://brew.sh)

```
brew install ko
```

### Build and Install from Source

With Go 1.16+, build and install the latest released version:

```
go install github.com/google/ko
```

## Authenticate

`ko` depends on the authentication configured in your Docker config (typically
`~/.docker/config.json`). If you can push an image with `docker push`, you are
already authenticated for `ko`.

Since `ko` doesn't require `docker`, `ko login` also provides a surface for
logging in to a container image registry with a username and password, similar
to
[`docker login`](https://docs.docker.com/engine/reference/commandline/login/).

## Choose Destination

`ko` depends on an environment variable, `KO_DOCKER_REPO`, to identify where it
should push images that it builds. Typically this will be a remote registry,
e.g.:

- `KO_DOCKER_REPO=gcr.io/my-project`, or
- `KO_DOCKER_REPO=my-dockerhub-user`

# Build an Image

`ko publish ./cmd/app` builds and pushes a container image, and prints the
resulting image digest to stdout.

```
ko publish ./cmd/app
...
gcr.io/my-project/app-099ba5bcefdead87f92606265fb99ac0@sha256:6e398316742b7aa4a93161dce4a23bc5c545700b862b43347b941000b112ec3e
```

Because the output of `ko publish` is an image reference, you can easily pass it
to other tools that expect to take an image reference:

To run the container:

```
docker run -p 8080:8080 $(ko publish ./cmd/app)
```

Or, for example, to deploy it to other services like
[Cloud Run](https://cloud.google.com/run):

```
gcloud run deploy --image=$(ko publish ./cmd/app)
```

## Configuration

Aside from `KO_DOCKER_REPO`, you can configure `ko`'s behavior using a
`.ko.yaml` file. The location of this file can be overridden with
`KO_CONFIG_PATH`.

### Overriding Base Images

By default, `ko` bases images on `gcr.io/distroless/static:nonroot`. This is a
small image that provides the bare necessities to run your Go binary.

You can override this base image in two ways:

1. To override the base image for all images `ko` builds, add this line to your
   `.ko.yaml` file:

```yaml
defaultBaseImage: registry.example.com/base/image
```

2. To override the base image for certain importpaths:

```yaml
baseImageOverrides:
  github.com/my-user/my-repo/cmd/app: registry.example.com/base/for/app
  github.com/my-user/my-repo/cmd/foo: registry.example.com/base/for/foo
```

## Naming Images

`ko` provides a few different strategies for naming the image it pushes, to
workaround certain registry limitations and user preferences:

Given `KO_DOCKER_REPO=registry.example.com/repo`, by default,
`ko publish ./cmd/app` will produce an image named like
`registry.example.com/repo/app-<md5>`, which includes the MD5 hash of the full
import path, to avoid collisions.

- `--preserve-import-path` (`-P`) will include the entire importpath:
  `registry.example.com/repo/github.com/my-user/my-repo/cmd/app`
- `--base-import-paths` (`-B`) will omit the MD5 portion:
  `registry.example.com/repo/app`
- `--bare` will only include the `KO_DOCKER_REPO`: `registry.example.com/repo`

## Local Publishing Options

`ko` is normally used to publish images to container image registries,
identified by `KO_DOCKER_REPO`.

`ko` can also publish images to a local Docker daemon, if available, by setting
`KO_DOCKER_REPO=ko.local`, or by passing the `--local` (`-L`) flag.

Locally-published images can be used as a base image for other `ko` images:

```yaml
defaultBaseImage: ko.local/example/base/image
```

`ko` can also publish images to a local [KinD](https://kind.sigs.k8s.io)
cluster, if available, by setting `KO_DOCKER_REPO=kind.local`. By default this
publishes to the default KinD cluster name (`kind`). To publish to another KinD
cluster, set `KIND_CLUSTER_NAME=my-other-cluster`.

## Multi-Platform Images

Because Go supports cross-compilation to other CPU architectures and operating
systems, `ko` excels at producing multi-platform images.

To build and push an image for all platforms supported by the configured base
image, simply add `--platform=all`. This will instruct `ko` to look up all the
supported platforms in the base image, execute
`GOOS=<os> GOARCH=<arch> GOARM=<variant> go build` for each platform, and
produce a manifest list containing an image for each platform.

You can also select specific platforms, for example,
`--platform=linux/amd64,linux/arm64`

## Static Assets

`ko` can also bundle static assets into the images it produces.

By convention, any contents of a directory named `<importpath>/kodata/` will be
bundled into the image, and the path where it's available in the image will be
identified by the environment variable `KO_DATA_PATH`.

As an example, you can bundle and serve static contents in your image:

```
cmd/
  app/
    main.go
    kodata/
      favicon.ico
      index.html
```

Then, in your `main.go`:

```go
func main() {
    http.Handle("/", http.FileServer(http.Dir(os.Getenv("KO_DATA_PATH"))))
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

You can simulate `ko`'s behavior outside of the container image by setting the
`KO_DATA_PATH` environment variable yourself:

```
KO_DATA_PATH=cmd/app/kodata/ go run ./cmd/app
```

**Tip:** Symlinks in `kodata` are followed and included as well. For example,
you can include Git commit information in your image with:

```
ln -s -r .git/HEAD ./cmd/app/kodata/
```

Also note that `http.FileServer` will not serve the `Last-Modified` header
(or validate `If-Modified-Since` request headers) because `ko` does not embed
timestamps by default.

This can be supported by manually setting the `KO_DATA_DATE_EPOCH` environment
variable during build ([See below](#Why-are-my-images-all-created-in-1970)).

# Kubernetes Integration

You could stop at just building and pushing images.

But, because building images is so _easy_ with `ko`, and because building with
`ko` only requires a string importpath to identify the image, we can integrate
this with YAML generation to make Kubernetes use cases much simpler.

## YAML Changes

Traditionally, you might have a Kubernetes deployment, defined in a YAML file,
that runs an image:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deployment
spec:
  replicas: 3
  ...
  template:
    spec:
      containers:
      - name: my-app
        image: registry.example.com/my-app:v1.2.3
```

...which you apply to your cluster with `kubectl apply`:

```
kubectl apply -f deployment.yaml
```

With `ko`, you can instead reference your Go binary by its importpath, prefixed
with `ko://`:

```yaml
    ...
    spec:
      containers:
      - name: my-app
        image: ko://github.com/my-user/my-repo/cmd/app
```

## `ko resolve`

With this small change, running `ko resolve -f deployment.yaml` will instruct
`ko` to:

1. scan the YAML file(s) for values with the `ko://` prefix,
2. for each unique `ko://`-prefixed string, execute `ko publish <importpath>` to
   build and push an image,
3. replace `ko://`-prefixed string(s) in the input YAML with the fully-specified
   image reference of the built image(s), for example:

```yaml ...
spec:
  containers:
    - name: my-app
      image: registry.example.com/github.com/my-user/my-repo/cmd/app@sha256:deadb33f...
```

4. Print the resulting resolved YAML to stdout.

The result can be redirected to a file, to distribute to others:

```
ko resolve -f config/ > release.yaml
```

Taken together, `ko resolve` aims to make packaging, pushing, and referencing
container images an invisible implementation detail of your Kubernetes
deployment, and let you focus on writing code in Go.

## `ko apply`

To apply the resulting resolved YAML config, you can redirect the output of
`ko resolve` to `kubectl apply`:

```
ko resolve -f config/ | kubectl apply -f -
```

Since this is a relatively common use case, the same functionality is available
using `ko apply`:

```
ko apply -f config/
```

**NB:** This requires that `kubectl` is available.

## `ko delete`

To teardown resources applied using `ko apply`, you can run `ko delete`:

```
ko delete -f config/
```

This is purely a convenient alias for `kubectl delete`, and doesn't perform any
builds, or delete any previously built images.

# Frequently Asked Questions

## How can I set `ldflags`?

[Using -ldflags](https://blog.cloudflare.com/setting-go-variables-at-compile-time/)
is a common way to embed version info in go binaries (In fact, we do this for
`ko`!). Unfortunately, because `ko` wraps `go build`, it's not possible to use
this flag directly; however, you can use the `GOFLAGS` environment variable
instead:

```
GOFLAGS="-ldflags=-X=main.version=1.2.3" ko publish .
```

## Why are my images all created in 1970?

In order to support [reproducible builds](https://reproducible-builds.org), `ko`
doesn't embed timestamps in the images it produces by default.

However, `ko` does respect the [`SOURCE_DATE_EPOCH`](https://reproducible-builds.org/docs/source-date-epoch/)
environment variable, which will set the container image's timestamp
accordingly.

Similarly, the `KO_DATA_DATE_EPOCH` environment variable can be used to set
the _modtime_ timestamp of the files in `KO_DATA_PATH`.

For example, you can set the container image's timestamp to the current
timestamp by executing:

```
export SOURCE_DATE_EPOCH=$(date +%s)
```

or set the timestamp of the files in `KO_DATA_PATH` to the latest git commit's
timestamp with:

```
export KO_DATA_DATE_EPOCH=$(git log -1 --format='%ct')
```

## Can I build Windows containers?

Yes, but support for Windows containers is new, experimental, and tenuous. Be
prepared to file bugs. 🐛

You can try out building a Windows container image by [setting the base image](#overriding-base-images) to
a Windows base image and building with `--platform=windows/amd64` or
`--platform=all`:

For example, to build a Windows container image for `ko`, from within this repo:

```
KO_DEFAULTBASEIMAGE=mcr.microsoft.com/windows/nanoserver:1809 ko publish ./ --platform=windows/amd64
```

## Can I optimize images for [eStargz support](https://github.com/containerd/stargz-snapshotter/blob/v0.2.0/docs/stargz-estargz.md)?

Yes! Set the environment variable `GGCR_EXPERIMENT_ESTARGZ=1` to produce
eStargz-optimized images.

## Does `ko` support autocompletion?

Yes! `ko completion` generates a Bash completion script, which you can add to
your `bash_completion` directory:

```
ko completion > /usr/local/etc/bash_completion.d/ko
```

Or, you can source it directly:

```bash
source <(ko completion)
```

## Does `ko` work with [Kustomize](https://kustomize.io/)?

Yes! `ko resolve -f -` will read and process input from stdin, so you can have
`ko` easily process the output of the `kustomize` command.

```
kustomize build config | ko resolve -f -
```

## Does `ko` work with [OpenShift Internal Registry](https://docs.openshift.com/container-platform/4.7/registry/registry-options.html#registry-integrated-openshift-registry_registry-options)?

Yes! Follow these steps:

- Connect to your OpenShift installation:
  https://docs.openshift.com/container-platform/4.7/cli_reference/openshift_cli/getting-started-cli.html#cli-logging-in_cli-developer-commands
- Expose the OpenShift Internal Registry so you can push to it:
  https://docs.openshift.com/container-platform/4.7/registry/securing-exposing-registry.html
- Export your token to `$HOME/.docker/config.json`:

```sh
oc registry login --to=$HOME/.docker/config.json
```

- Create a namespace where you will push your images, i.e: `ko-images`
- Execute this command to set `KO_DOCKER_REPO` to publish images to the internal
  registry.

```sh
   export KO_DOCKER_REPO=$(oc get route default-route -n openshift-image-registry --template='{{ .spec.host }}')/ko-images
```

# Acknowledgements

This work is based heavily on learnings from having built the
[Docker](https://github.com/bazelbuild/rules_docker) and
[Kubernetes](https://github.com/bazelbuild/rules_k8s) support for
[Bazel](https://bazel.build). That work was presented
[here](https://www.youtube.com/watch?v=RS1aiQqgUTA).

# Discuss

Questions? Comments? Ideas? Come discuss `ko` with us in the `#ko-project`
channel on the [Kubernetes Slack](https://slack.k8s.io)! See you there!
