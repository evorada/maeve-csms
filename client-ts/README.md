# MaEVe CSMS TypeScript Client

TypeScript client library for the MaEVe CSMS API, generated from the OpenAPI specification.

## Installation

```bash
npm install @evorada/maeve-csms-client
```

## Usage

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

## Development

### Generate Client

```bash
npm run generate
```

### Build

```bash
npm run build
```

### Clean

```bash
npm run clean
```

## License

Apache 2.0
