// Special target: https://github.com/docker/metadata-action#bake-definition
target "docker-metadata-action" {}

target "image" {
  inherits  = ["docker-metadata-action"]
  platforms = [
    "linux/amd64",
    "linux/arm64",
    "linux/arm/v7",
  ]
}

target "artifact" {
  target = "artifact"
  output = ["./dist"]
  platforms = [
    "linux/amd64",
    "linux/arm64",
    "linux/arm/v7",
    "darwin/amd64",
    "darwin/arm64",
    "windows/amd64",
    "windows/arm64",
  ]
}
