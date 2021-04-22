# `oci-image` prototype (prototype)

A (prototype) Concourse prototype for building [OCI
images](https://github.com/opencontainers/image-spec). Currently uses
[`buildkit`](http://github.com/moby/buildkit) for building.

~~Code stolen shamelessly from~~ Adapted from vito/oci-build-task.

Not gonna document usage because you can't use this in Concourse yet!

However, it can build itself locally by running:

```sh
jq -n '{object: {context: "input", output: "image", cache: true}, response_path: "/dev/null"}' | \
  docker run --rm -i \
  --privileged \
  -w /workdir \
  -v "$(pwd):/workdir/input" \
  -v "/tmp/output:/workdir/image" \
  -v "/tmp/cache:/workdir/cache" \
  aoldershaw/oci-image-prototype build
```

...and it'll be built to `/tmp/output/image.tar`.
