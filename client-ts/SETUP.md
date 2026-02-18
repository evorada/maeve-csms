# TypeScript Client Setup

Before the release workflow can publish the TypeScript client, you need to configure the GitLab project ID and create a GitLab access token.

## Step 1: Get GitLab Project ID

1. Go to your GitLab project: https://gitlab.com/evorada/maeve-csms
2. The project ID is shown in the project overview page (under the project name)
3. Or, get it via API:
   ```bash
   curl --header "PRIVATE-TOKEN: your_token" \
        "https://gitlab.com/api/v4/projects/evorada%2Fmaeve-csms" | jq .id
   ```

## Step 2: Update Package Configuration

Once you have the project ID, update these files:

### client-ts/package.json

Replace `65031632` with your actual project ID:

```json
"publishConfig": {
  "@evorada:registry": "https://gitlab.com/api/v4/projects/YOUR_PROJECT_ID/packages/npm/"
}
```

### .github/workflows/release.yml

Replace `65031632` with your actual project ID in two places:

```yaml
- name: Setup Node.js
  uses: actions/setup-node@v4
  with:
    node-version: '20'
    registry-url: 'https://gitlab.com/api/v4/projects/YOUR_PROJECT_ID/packages/npm/'
```

And in the release notes:

```yaml
@evorada:registry=https://gitlab.com/api/v4/projects/YOUR_PROJECT_ID/packages/npm/
```

### client-ts/PUBLISHING.md

Update all references to project ID `65031632` with your actual project ID.

## Step 3: Create GitLab Access Token

1. Go to: https://gitlab.com/-/user_settings/personal_access_tokens
2. Create a new token:
   - **Name**: `maeve-csms-npm-publish`
   - **Scopes**: `api` (required)
   - **Expiration**: Set according to your policy
3. Copy the token immediately

## Step 4: Add GitHub Secret

1. Go to: https://github.com/evorada/maeve-csms/settings/secrets/actions
2. Click "New repository secret"
3. Add:
   - **Name**: `GITLAB_TOKEN`
   - **Value**: Paste the GitLab access token from Step 3
4. Click "Add secret"

## Step 5: Test the Setup

You can test the TypeScript client build locally:

```bash
cd client-ts
npm install
npm run build
```

This will generate the client from the OpenAPI spec and compile it to JavaScript.

## Verification Checklist

Before creating a release tag, verify:

- [ ] GitLab project ID updated in all configuration files
- [ ] `GITLAB_TOKEN` secret configured in GitHub repository
- [ ] Token has `api` scope
- [ ] Local build works (`npm run build` in client-ts/)
- [ ] OpenAPI spec is valid (`manager/api/api-spec.yaml`)

## Troubleshooting

### "Cannot find module '@openapitools/openapi-generator-cli'"

Run `npm install` in the `client-ts/` directory first.

### "API spec validation failed"

Check the OpenAPI spec in `manager/api/api-spec.yaml` for syntax errors.

### "403 Forbidden" when publishing

- Verify the GitLab token has `api` scope
- Check if the token has expired
- Ensure the project ID is correct
- Verify you have maintainer/owner access to the GitLab project

## Next Steps

After completing this setup, the TypeScript client will be automatically published to the GitLab npm registry whenever you create a release tag.

See [PUBLISHING.md](PUBLISHING.md) for information about the publishing process and how to install the published package.
