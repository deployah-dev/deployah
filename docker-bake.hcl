// Special target: https://github.com/docker/metadata-action#bake-definition
target "docker-metadata-action" {}

variable "VERSION" {
  default = "dev"
}

target "image" {
  inherits  = ["docker-metadata-action"]
  args = {
    VERSION = VERSION
  }
  platforms = [
    "linux/amd64",
    "linux/arm64",
    "linux/arm/v7",
  ]
}

target "artifact" {
  target = "artifact"
  output = ["./dist"]
  args = {
    VERSION = VERSION
  }
  platforms = [
    "linux/amd64",
    "linux/arm64",
    "linux/arm/v7",
    "darwin/amd64",
    "darwin/arm64",
    // TODO: add windows targets once platform-specific code (syscall.SIGWINCH,
    //       renameio.WriteFile) is guarded with build tags.
    // "windows/amd64",
    // "windows/arm64",
  ]
}
