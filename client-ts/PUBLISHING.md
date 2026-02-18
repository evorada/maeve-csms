# Publishing TypeScript Client to GitLab npm Registry

## Overview

The TypeScript client is automatically built and published to the GitLab npm registry as part of the release process when a version tag is pushed.

## Prerequisites

### GitLab Access Token

The release workflow requires a GitLab access token with `api` scope to publish packages to the GitLab npm registry.

#### Creating the Token

1. Go to GitLab: https://gitlab.com/-/user_settings/personal_access_tokens
2. Create a new token with:
   - **Name**: `maeve-csms-npm-publish` (or similar)
   - **Scopes**: `api` (required for npm registry access)
   - **Expiration**: Set according to your security policy
3. Copy the token immediately (you won't be able to see it again)

#### Configuring GitHub Secret

1. Go to your GitHub repository: https://github.com/evorada/maeve-csms/settings/secrets/actions
2. Click "New repository secret"
3. Add:
   - **Name**: `GITLAB_TOKEN`
   - **Value**: Paste the GitLab access token
4. Click "Add secret"

## Release Process

When you push a version tag (format: `YYYY.MM.DD`), the release workflow will:

1. **Generate TypeScript client** from the OpenAPI spec (`manager/api/api-spec.yaml`)
2. **Update package version** to match the git tag
3. **Install dependencies** (openapi-generator-cli, typescript, etc.)
4. **Build the package** (compile TypeScript to JavaScript)
5. **Publish to GitLab npm registry** using the `GITLAB_TOKEN` secret

### Example

```bash
# Tag a new release
git tag 2026.02.18
git push origin 2026.02.18

# GitHub Actions will automatically:
# - Build binaries for manager and gateway
# - Build and push Docker images
# - Build and publish TypeScript client to GitLab npm
# - Create GitHub release with all artifacts
```

## Installing the Published Package

### Configure npm to use GitLab registry

Add to your project's `.npmrc`:

```
@evorada:registry=https://gitlab.com/api/v4/projects/65031632/packages/npm/
```

### Install the package

```bash
npm install @evorada/maeve-csms-client@2026.02.18
```

Or for the latest version:

```bash
npm install @evorada/maeve-csms-client@latest
```

## Manual Publishing (Development)

If you need to publish manually for testing:

### 1. Authenticate with GitLab

```bash
npm config set @evorada:registry https://gitlab.com/api/v4/projects/65031632/packages/npm/
echo "//gitlab.com/api/v4/projects/65031632/packages/npm/:_authToken=${GITLAB_TOKEN}" >> ~/.npmrc
```

### 2. Build and publish

```bash
cd client-ts
npm install
npm run build
npm publish
```

## Package Registry URL

The TypeScript client is published to:

```
https://gitlab.com/evorada/maeve-csms/-/packages
```

You can view all published versions there.

## Troubleshooting

### Authentication Errors

If you see authentication errors during publishing:

1. Verify the `GITLAB_TOKEN` secret is set correctly in GitHub
2. Ensure the token has `api` scope
3. Check if the token has expired

### Version Conflicts

If a version already exists, you'll get an error. GitLab npm registry doesn't allow overwriting published versions. Create a new tag with a different version number.

### Build Failures

Check the GitHub Actions logs for detailed error messages:
https://github.com/evorada/maeve-csms/actions

Common issues:
- OpenAPI spec validation errors
- TypeScript compilation errors
- Missing dependencies

## Version Strategy

The project uses EVerest-style date versioning:

- Format: `YYYY.MM.DD`
- Example: `2026.02.18`
- Multiple releases on the same day: `2026.02.18-1`, `2026.02.18-2`, etc.

## Additional Resources

- [GitLab npm Registry Docs](https://docs.gitlab.com/ee/user/packages/npm_registry/)
- [OpenAPI Generator TypeScript Axios](https://openapi-generator.tech/docs/generators/typescript-axios/)
- [GitHub Actions Secrets](https://docs.github.com/en/actions/security-guides/encrypted-secrets)
