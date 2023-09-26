# Trusted OS Release Process

TODO(jayhou): This file contains the design of the release process. It is in the
process of being implemented and may not be accurate of the current state.

## File structure

*   The Dockerfile found in the root of the repo builds an image which installs
    dependencies and compiles the Trusted OS with TamaGo. The version of TamaGo
    to use can be specified with the Docker
    [build arg](https://docs.docker.com/engine/reference/commandline/build/#build-arg)
    `TAMAGO_VERSION`.
*   [Cloud Build triggers](https://cloud.google.com/build/docs/automating-builds/create-manage-triggers)
    for the continuous integration (CI) and prod environments are defined on the
    Cloud Build yaml files in this directory.

## Build and Release Process

There are three parts to the Trusted OS release process.

### Release kickoff

First, the trigger defined on `cloudbuild(|_ci).yaml` file
is defined by a yaml config file and is invoked when the Transparency.dev team
publishes a new tag in the format `vX.X.X` in this repository.

This trigger builds and writes the Trusted OS ELF file to a public Google Cloud
Storage (GCS) bucket. Then, it runs the
[`manifest`](https://github.com/transparency-dev/armored-witness/tree/main/cmd/manifest)
tool to construct the Claimant Model Statement with arguments specific to this
release, and writes it to the same GCS bucket. Then, Transparency.dev signs the
output manifest file in the
[note](https://pkg.go.dev/golang.org/x/mod/sumdb/note) format.

Since it is stored in the public GCS bucket, it can be read by WithSecure.

### WithSecure step

WithSecure is notified of a release, and they reference the manifest for build
details. After auditing it, and they add their signature of the manifest to the
note as well before writing it to this repo. Once complete, they tag a release
in this repo in the format `withsecure_vX.X.X`.

### Release completion

Finally, the trigger defined on `cloudbuild_withsecure_signature.yaml` reads the
signed note written to this repository by WithSecure and adds it as an entry to
the public firmware transparency log.

TODO: add links for the GCS buckets once public.

## Claimant Model

| Role         | Description |
| -----------  | ----------- |
| **Claimant** | <ul><li>For Claims #1, #2: Transparency.dev team</li><li>For Claims #1, #3: WithSecure</li></ul> |
| **Claim**    | <ol><li>The digest of the Trusted OS firmware is derived from this source Github repository, and is reproducible.</li><li>The Trusted OS firmware is issued by the Transparency.dev team.</li><li>The Trusted OS firmware is issued by the WithSecure.</li></ol> |
| **Believer** | Armored Witness devices |
| **Verifier** | <ul><li>For Claim #1: third party auditing the Transparency.dev team and WithSecure</li><li>For Claim #2: the Transparency.dev team</li><li>For Claim #3: WithSecure</li></ul> |
| **Arbiter**  | Log ecosystem participants and reliers |

The **Statement** is defined in the [armored-witness-common](https://github.com/transparency-dev/armored-witness-common/blob/main/release/firmware/ftlog/log_entries.go) repo.
There is also an example available at
[example_firmware_release.json](https://github.com/transparency-dev/armored-witness-common/blob/main/release/firmware/ftlog/example_firmware_release.json).