# Hello Function

Minimal OCI Functions-compatible Python function for the managed Function demo.

The function accepts JSON input and returns JSON output:

```json
{
  "ok": true,
  "greeting": "hello from oke functions operator",
  "input": {
    "name": "Ron",
    "index": 0
  }
}
```

The `greeting` value comes from the `GREETING` function config/environment variable.

## Build On Apple Silicon

Build the function image for `linux/amd64`:

```sh
podman build --platform linux/amd64 -t ghcr.io/ronsevet/oci-functions-operator/hello-function:dev examples/hello-function
```

## Push To GHCR

Log in if needed:

```sh
podman login ghcr.io
```

Push the image:

```sh
podman push ghcr.io/ronsevet/oci-functions-operator/hello-function:dev
```

Use this image as `<FUNCTION_IMAGE>` in `config/samples/functions_v1alpha1_function_managed.yaml`.
