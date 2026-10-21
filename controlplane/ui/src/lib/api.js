async function request(path, options = {}) {
  let response;
  try {
    response = await fetch(path, {
      headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
      ...options
    });
  } catch (error) {
    throw new Error(`Control plane tidak dapat dihubungi. Pastikan API berjalan di http://localhost:8080 (${error.message})`);
  }
  if (!response.ok) {
    let detail = `${response.status} ${response.statusText}`;
    try {
      const problem = await response.json();
      detail = problem.detail || problem.title || detail;
    } catch {
      // Keep the HTTP status when the server did not return JSON.
    }
    const error = new Error(detail);
    error.status = response.status;
    throw error;
  }
  if (response.status === 204) return null;
  return response.json();
}

export const api = {
  health: () => request('/healthz'),
  version: () => request('/v1/version'),
  tenants: () => request('/v1/tenants'),
  createTenant: (body) => request('/v1/tenants', { method: 'POST', body: JSON.stringify(body) }),
  updateTenant: (id, body) => request(`/v1/tenants/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  deleteTenant: (id) => request(`/v1/tenants/${id}`, { method: 'DELETE' }),
  connections: (tenantId) => request(`/v1/connections?tenant_id=${encodeURIComponent(tenantId)}`),
  createConnection: (body) => request('/v1/connections', { method: 'POST', body: JSON.stringify(body) }),
  updateConnection: (id, body) => request(`/v1/connections/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  deleteConnection: (id) => request(`/v1/connections/${id}`, { method: 'DELETE' }),
  testConnection: (body) => request('/v1/connections/test', { method: 'POST', body: JSON.stringify(body) }),
  connectors: (tenantId = '') => request(`/v1/connectors${tenantId ? `?tenant_id=${encodeURIComponent(tenantId)}` : ''}`),
  pipelines: (tenantId) => request(`/v1/pipelines?tenant_id=${encodeURIComponent(tenantId)}`),
  createPipeline: (body) => request('/v1/pipelines', { method: 'POST', body: JSON.stringify(body) }),
  updatePipeline: (id, body) => request(`/v1/pipelines/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  deletePipeline: (id) => request(`/v1/pipelines/${id}`, { method: 'DELETE' }),
  validatePipeline: (id) => request(`/v1/pipelines/${id}/validate`, { method: 'POST' }),
  runs: () => request('/v1/runs?limit=50'),
  runLogs: (id, limit = 200) => request(`/v1/runs/${id}/logs?limit=${limit}`),
  run: (pipelineId) => request(`/v1/pipelines/${pipelineId}/runs`, {
    method: 'POST',
    headers: { 'Idempotency-Key': crypto.randomUUID() },
    body: JSON.stringify({})
  }),
  cancelRun: (id) => request(`/v1/runs/${id}/cancel`, { method: 'POST' })
};
