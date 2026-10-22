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
    if (response.status === 401 && path !== '/v1/auth/login') {
      window.dispatchEvent(new CustomEvent('auth:required'));
    }
    throw error;
  }
  if (response.status === 204) return null;
  return response.json();
}

function createUUID() {
  if (globalThis.crypto?.randomUUID) {
    return globalThis.crypto.randomUUID();
  }

  const bytes = new Uint8Array(16);
  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(bytes);
  } else {
    for (let index = 0; index < bytes.length; index += 1) {
      bytes[index] = Math.floor(Math.random() * 256);
    }
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, '0'));
  return [
    hex.slice(0, 4).join(''),
    hex.slice(4, 6).join(''),
    hex.slice(6, 8).join(''),
    hex.slice(8, 10).join(''),
    hex.slice(10, 16).join('')
  ].join('-');
}

export const api = {
  health: () => request('/healthz'),
  version: () => request('/v1/version'),
  login: (username, password) => request('/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password })
  }),
  logout: () => request('/v1/auth/logout', { method: 'POST' }),
  me: () => request('/v1/auth/me'),
  changePassword: (currentPassword, newPassword) => request('/v1/auth/password', {
    method: 'POST',
    body: JSON.stringify({
      current_password: currentPassword,
      new_password: newPassword
    })
  }),
  users: () => request('/v1/users'),
  createUser: (body) => request('/v1/users', { method: 'POST', body: JSON.stringify(body) }),
  updateUser: (id, body) => request(`/v1/users/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  deleteUser: (id) => request(`/v1/users/${id}`, { method: 'DELETE' }),
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
    headers: { 'Idempotency-Key': createUUID() },
    body: JSON.stringify({})
  }),
  cancelRun: (id) => request(`/v1/runs/${id}/cancel`, { method: 'POST' })
};
