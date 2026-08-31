# Nginx example

Deploys the public `nginx:1.26` image to a local Deployah cluster with HTTPS
via `expose: true`.

## Run it

You need Docker or Podman. From the repo root:

```sh
cd examples/nginx
deployah cluster up
deployah deploy local --yes
deployah cluster status
```

`deployah cluster up` creates `deployah.platform.yaml` in this directory with a
`local` environment. Open the URL printed by `deployah cluster status` to see
the nginx welcome page.

## Clean up

```sh
deployah delete nginx local --yes
deployah cluster down --force
```
