# Releasing Maeve CSMS

This document describes the release process for Maeve CSMS.

## Release Naming

We follow the **EVerest release naming scheme**, using dates in the format:

```
YYYY.MM.DD
```

Example: `2026.02.17`

## Creating a Release

1. **Ensure all changes are merged to `main`**
   ```bash
   git checkout main
   git pull origin main
   ```

2. **Create and push a release tag**
   ```bash
   # Format: YYYY.MM.DD
   TAG="2026.02.17"
   git tag -a "${TAG}" -m "Release ${TAG}"
   git push origin "${TAG}"
   ```

3. **GitHub Actions will automatically:**
   - Build binaries for multiple platforms (Linux, macOS, Windows) and architectures (amd64, arm64)
   - Build and push Docker images for both `manager` and `gateway` to GitHub Container Registry
   - Build and publish the TypeScript client to GitLab npm registry
   - Create a GitHub Release with all artifacts

4. **Review and publish the release**
   - Go to the Releases page on GitHub
   - The release will be created automatically
   - Review the release notes and assets
   - Publish if everything looks correct

## Release Artifacts

Each release includes:

### TypeScript Client

The TypeScript client is published to the GitLab npm registry:
- Package: `@evorada/maeve-csms-client@YYYY.MM.DD`
- Registry: `https://gitlab.com/api/v4/projects/65031632/packages/npm/`

See [client-ts/PUBLISHING.md](client-ts/PUBLISHING.md) for detailed installation and usage instructions.

### Docker Images
- `ghcr.io/evorada/maeve-csms-manager:YYYY.MM.DD`
- `ghcr.io/evorada/maeve-csms-gateway:YYYY.MM.DD`
- `ghcr.io/evorada/maeve-csms-manager:latest` (main branch only)
- `ghcr.io/evorada/maeve-csms-gateway:latest` (main branch only)

Multi-platform support:
- `linux/amd64`
- `linux/arm64`

### Binaries

For both `manager` and `gateway`:

**Linux:**
- `manager-YYYY.MM.DD-linux-amd64.tar.gz`
- `manager-YYYY.MM.DD-linux-arm64.tar.gz`
- `gateway-YYYY.MM.DD-linux-amd64.tar.gz`
- `gateway-YYYY.MM.DD-linux-arm64.tar.gz`

**macOS:**
- `manager-YYYY.MM.DD-darwin-amd64.tar.gz` (Intel)
- `manager-YYYY.MM.DD-darwin-arm64.tar.gz` (Apple Silicon)
- `gateway-YYYY.MM.DD-darwin-amd64.tar.gz` (Intel)
- `gateway-YYYY.MM.DD-darwin-arm64.tar.gz` (Apple Silicon)

**Windows:**
- `manager-YYYY.MM.DD-windows-amd64.zip`
- `gateway-YYYY.MM.DD-windows-amd64.zip`

## Using Docker Images

### Pull the images

```bash
docker pull ghcr.io/evorada/maeve-csms-manager:2026.02.17
docker pull ghcr.io/evorada/maeve-csms-gateway:2026.02.17
```

### Or use in docker-compose.yml

```yaml
services:
  manager:
    image: ghcr.io/evorada/maeve-csms-manager:2026.02.17
    # ... rest of config
  
  gateway:
    image: ghcr.io/evorada/maeve-csms-gateway:2026.02.17
    # ... rest of config
```

## Using Binaries

### Linux/macOS

```bash
# Download and extract
wget https://github.com/evorada/maeve-csms/releases/download/2026.02.17/manager-2026.02.17-linux-amd64.tar.gz
tar xzf manager-2026.02.17-linux-amd64.tar.gz

# Run
./manager
```

### Windows

```powershell
# Download and extract the .zip file
# Then run:
.\manager.exe
```

## Using the TypeScript Client

### Configure npm

Add to your project's `.npmrc`:

```
@evorada:registry=https://gitlab.com/api/v4/projects/65031632/packages/npm/
```

### Install

```bash
npm install @evorada/maeve-csms-client@2026.02.17
```

### Usage

```typescript
import { DefaultApi, Configuration } from '@evorada/maeve-csms-client';

const config = new Configuration({
  basePath: 'http://localhost:9410/api/v0',
});

const client = new DefaultApi(config);

// Register a charge station
await client.registerChargeStation('CS001', {
  securityProfile: 0,
  base64SHA256Password: 'password_hash_here',
});
```

See [client-ts/README.md](client-ts/README.md) for more examples.

## Hotfix Releases

For hotfix releases on the same day, append a patch number:

```
YYYY.MM.DD.N
```

Example: `2026.02.17.1`, `2026.02.17.2`

## Troubleshooting

### Docker image not found

Make sure the package is public or you're authenticated:

```bash
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin
```

### Release workflow failed

Check the Actions tab on GitHub for detailed logs. Common issues:
- Build failures (check tests pass locally first)
- Docker authentication (ensure GITHUB_TOKEN has package write permissions)
- TypeScript client publishing (ensure GITLAB_TOKEN secret is set with `api` scope)
- Tag format incorrect (must match `YYYY.MM.DD` or `YYYY.MM.DD.N`)

### TypeScript client authentication issues

If publishing to GitLab npm registry fails:
1. Verify the `GITLAB_TOKEN` secret is set in GitHub repository settings
2. Ensure the token has `api` scope
3. Check if the token has expired
4. See [client-ts/PUBLISHING.md](client-ts/PUBLISHING.md) for detailed setup instructions
