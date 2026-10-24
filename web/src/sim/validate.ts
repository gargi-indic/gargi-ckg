import { apiClient } from '../api/client';
import type { definitions } from '../api/schema';

export type SimDocs = { graph: string; sim: string; experiment: string };
export type ValidationIssue = definitions['v1ValidationIssue'];

type ValidateRequest = definitions['v1ValidateModelRequest'];
type ValidateResponse = definitions['v1ValidateModelResponse'];

// schema.ts is generated from a Swagger 2 spec, which openapi-fetch's path
// typing does not understand (bodies infer as undefined). This narrow view
// restores the real request and response types from the generated definitions.
type ValidateClient = {
  POST(
    path: '/v1/models:validate',
    init: { body: ValidateRequest },
  ): Promise<{
    data?: ValidateResponse;
    error?: definitions['rpcStatus'];
    response: Response;
  }>;
};

/**
 * validateModel posts the sim and graph documents to POST /v1/models:validate
 * and returns the reported issues. Network and HTTP failures throw an Error.
 */
export async function validateModel(docs: SimDocs): Promise<ValidationIssue[]> {
  const client = apiClient as unknown as ValidateClient;
  const res = await client.POST('/v1/models:validate', {
    body: { sim: docs.sim, graph: docs.graph, format: 'yaml' },
  });
  if (res.error !== undefined || !res.data) {
    throw new Error(res.error?.message || `Validation request failed (HTTP ${res.response.status})`);
  }
  return res.data.errors ?? [];
}
