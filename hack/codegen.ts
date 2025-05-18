import OASNormalize from 'oas-normalize';
import { writeFile } from 'node:fs/promises';

const path = `${import.meta.dir}/../upstream/api-schemas/openapi.json`;

const doc = new OASNormalize(path, {
  enablePaths: true,
  parser: {
    dereference: {
      circular: 'ignore',
    },
  },
});

const derefed = await doc.dereference();

await writeFile('tmp.json', JSON.stringify(derefed));
