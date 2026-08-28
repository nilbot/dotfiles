# Why does `brew test-bot --only-tap-syntax` pass on broken formulas?

## The Question

If a formula in a Homebrew tap has placeholder checksums (e.g. `sha256 "000000..."`) or points to a release URL that has not yet been published, why does Homebrew's official CI runner (`brew test-bot --only-tap-syntax`) still report 100% green?

## The Finding

`brew test-bot --only-tap-syntax` is a purely **static and syntactic audit**. It runs:
1. `brew style <tap>`: RuboCop linting for Homebrew Ruby DSL conventions (frozen string literals, indentation, description formatting).
2. `brew readall --aliases --os=all --arch=all <tap>`: Evaluates formula Ruby class definitions across simulated OS and architecture matrices to ensure every branch provides a `url`.
3. `brew audit --except=installed --tap=<tap>`: Static structure checks (redundant version tags, license formatting, URL syntax).

**What it does NOT do:**
- It **does not download** the URL.
- It **does not verify** that the remote file exists (no HTTP `HEAD` or `GET`).
- It **does not verify** that the SHA-256 digest matches the actual binary archive.
- It **does not execute** `brew install` or `brew test`.

## The Trap

Relying solely on `brew test-bot --only-tap-syntax` CI passes creates a false sense of security. A formula can be 100% green in tap CI while being completely broken (404 Not Found or checksum mismatch) for any real user running `brew install`.

## The Required Verification Protocol

Real end-to-end verification of a Homebrew formula requires:
1. **Live Asset Publishing**: Packaging the actual release archives and publishing them to GitHub Releases.
2. **Cryptographic Checksum Extraction**: Extracting the true SHA-256 digests generated from the release build (`checksums.txt`).
3. **Live Network Install**: Running `brew update && brew install <user>/<tap>/<formula>` over the public network.
4. **Formula Test Execution**: Executing `brew test <user>/<tap>/<formula>` to verify the binary executes within Homebrew's prefix.
