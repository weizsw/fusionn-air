## 1. Clean up CI workflow

- [x] 1.1 Remove the `docker` job from `.github/workflows/ci.yml` (keep only `test` and `lint` jobs)

## 2. Create build workflow

- [x] 2.1 Create `.github/workflows/build.yml` with triggers on `push` to all branches (`**`) and tags (`v*`)
- [x] 2.2 Add Docker setup steps (checkout, QEMU, Buildx)
- [x] 2.3 Add login steps for both GHCR and Docker Hub
- [x] 2.4 Configure `docker/metadata-action` with branch-name tag (`type=ref,event=branch`), semver tags (`type=semver`), and `latest` only on version tags
- [x] 2.5 Add `docker/build-push-action` with multi-arch build (amd64+arm64), conditional VERSION build arg (ref_name for tags, sha for branches), and GHA cache
- [x] 2.6 Add GitHub Release step with `if: startsWith(github.ref, 'refs/tags/v')` using `softprops/action-gh-release@v2`

## 3. Remove release workflow

- [x] 3.1 Delete `.github/workflows/release.yml`
