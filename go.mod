module github.com/google/ko

go 1.14

require (
	github.com/containerd/stargz-snapshotter/estargz v0.0.0-20201217071531-2b97b583765b
	github.com/docker/cli v0.0.0-20200303162255-7d407207c304 // indirect
	github.com/docker/docker v1.4.2-0.20190924003213-a8608b5b67c7
	github.com/dprotaso/go-yit v0.0.0-20191028211022-135eb7262960
	github.com/evanphx/json-patch v4.2.0+incompatible // indirect
	github.com/fsnotify/fsnotify v1.4.9
	github.com/go-training/helloworld v0.0.0-20200225145412-ba5f4379d78b
	github.com/google/go-cmp v0.4.1
	github.com/google/go-containerregistry v0.2.2-0.20201217211853-687e0e365894
	github.com/googleapis/gnostic v0.4.0 // indirect
	github.com/gregjones/httpcache v0.0.0-20190212212710-3befbb6ad0cc // indirect
	github.com/json-iterator/go v1.1.10 // indirect
	github.com/mattmoor/dep-notify v0.0.0-20190205035814-a45dec370a17
	github.com/spf13/cobra v1.0.0
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.4.0
	golang.org/x/sync v0.0.0-20201207232520-09787c993a3a
	golang.org/x/text v0.3.3 // indirect
	golang.org/x/tools v0.0.0-20200924205911-8a9a89368bd3
	gopkg.in/yaml.v3 v3.0.0-20200615113413-eeeca48fe776
	gotest.tools/v3 v3.0.2 // indirect
	k8s.io/apimachinery v0.18.8
	k8s.io/cli-runtime v0.18.8
	sigs.k8s.io/kind v0.8.1
)

replace (
	k8s.io/api => k8s.io/api v0.18.8
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.8
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.18.8
	k8s.io/client-go => k8s.io/client-go v0.18.8
)
