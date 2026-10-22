<script>
  import { onMount } from 'svelte';
  import { api } from './lib/api.js';
  import Modal from './lib/Modal.svelte';
  import ConfirmDialog from './lib/ConfirmDialog.svelte';
  import ConnectionGroup from './lib/ConnectionGroup.svelte';
  import DatabaseIcon from './lib/DatabaseIcon.svelte';
  import Icon from './lib/Icon.svelte';

  const nav = [
    { name: 'Overview', icon: 'overview' },
    { name: 'Pipelines', icon: 'pipelines' },
    { name: 'Connections', icon: 'connections' },
    { name: 'Connectors', icon: 'connectors' },
    { name: 'Users', icon: 'users' }
  ];

  const driverLabels = {
    postgres: 'PostgreSQL',
    mysql: 'MySQL',
    mssql: 'SQL Server',
    oracle: 'Oracle',
    mongodb: 'MongoDB',
    rest: 'REST API',
    csv: 'CSV',
    'google-sheets': 'Google Sheets',
    s3: 'Amazon S3',
    duckdb: 'DuckDB'
  };

  const statusStyles = {
    succeeded: 'bg-emerald-50 text-emerald-700',
    running: 'bg-sky-50 text-sky-700',
    queued: 'bg-amber-50 text-amber-700',
    failed: 'bg-rose-50 text-rose-700',
    timed_out: 'bg-orange-50 text-orange-700',
    lost: 'bg-rose-50 text-rose-700',
    cancelled: 'bg-slate-100 text-slate-600',
    cancelling: 'bg-orange-50 text-orange-700'
  };

  const blankTenant = () => ({ id: '', name: '', display_name: '', environment_name: '' });
  const blankConnection = (kind = 'source') => ({
    id: '',
    name: '',
    kind,
    driver: 'postgres',
    host: '',
    port: '5432',
    database: '',
    schema: 'public',
    service_name: '',
    sid: '',
    sslmode: 'prefer',
    auth_source: 'admin',
    username: '',
    secret_ref: '',
    password: '',
    load_method: 'upsert',
    api_url: '',
    api_path: '',
    stream_name: '',
    rest_primary_keys: '',
    rest_records_path: '$[*]',
    auth_method: 'no_auth',
    api_key_header: 'Authorization',
    sheet_id: '',
    child_sheet_name: '',
    sheet_range: '',
    csv_path: '',
    csv_entity: '',
    csv_keys: '',
    csv_encoding: 'utf-8',
    s3_bucket: '',
    s3_region: '',
    s3_prefix: '',
    s3_format: 'parquet',
    s3_endpoint: '',
    duckdb_path: '',
    duckdb_schema: 'main'
  });
  const blankPipeline = () => ({
    id: '',
    name: '',
    extractor: '',
    loader: '',
    source_connection_id: '',
    destination_connection_id: '',
    state_id_suffix: '',
    default_timeout_seconds: 3600,
    max_attempts: 3,
    enabled: true,
    source_streams: '',
    row_filter: '',
    transfer_batch_size: '50000',
    transfer_validate_records: true,
    transfer_load_method: ''
  });

  let page = pageFromHash();
  let tenants = [];
  let selectedTenant = null;
  let connections = [];
  let pipelines = [];
  let runs = [];
  let connectors = [];
  let connectorSource = 'catalog';
  let loading = true;
  let busy = false;
  let refreshing = false;
  let error = '';
  let toast = '';
  let toastTimer;
  let lastUpdatedAt = null;
  let executionRunId = '';
  let executionDetail = null;
  let executionLoading = false;
  let executionError = '';
  let pipelineSearch = '';
  let pipelineFilter = 'all';
  let connectionSearch = '';
  let connectionFilter = 'all';
  let connectorSearch = '';
  let connectorFilter = 'all';
  let authLoading = true;
  let authenticated = false;
  let currentUser = null;
  let loginUsername = 'superadmin';
  let loginPassword = '';
  let loginBusy = false;
  let loginError = '';
  let users = [];
  let usersLoading = false;
  let showUserModal = false;
  let editingUser = null;
  let userSaving = false;
  let userStatus = '';
  let userForm = { username: '', display_name: '', password: '', role: 'admin' };
  let showPasswordModal = false;
  let passwordSaving = false;
  let passwordStatus = '';
  let passwordForm = { current_password: '', new_password: '', confirm_password: '' };

  let showTenantModal = false;
  let showConnectionModal = false;
  let showPipelineModal = false;
  let editingTenant = null;
  let editingConnection = null;
  let editingPipeline = null;
  let tenantForm = blankTenant();
  let connectionForm = blankConnection();
  let pipelineForm = blankPipeline();
  let connectionSaving = false;
  let connectionStatus = '';
  let connectionReady = false;
  let pipelineStatus = '';
  let deleteTarget = null;
  let deleteBusy = false;
  let stopTarget = null;
  let stopBusy = false;

  $: visibleNav = nav.filter((item) => item.name !== 'Users' || currentUser?.role === 'superadmin');

  $: sourceConnections = connections.filter((item) => item.kind === 'source');
  $: destinationConnections = connections.filter((item) => item.kind === 'destination');
  $: activeRuns = runs.filter((item) => ['queued', 'running', 'cancelling'].includes(item.status));
  $: successRuns = runs.filter((item) => item.status === 'succeeded');
  $: failedRuns = runs.filter((item) => ['failed', 'timed_out', 'lost'].includes(item.status));
  $: pipelineSourceDriver = selectedConnectionDriver(pipelineForm.source_connection_id);
  $: pipelineDestinationDriver = selectedConnectionDriver(pipelineForm.destination_connection_id);
  $: extractorOptions = availableConnectors('extractor').filter((connector) => connectorMatchesDriver(connector, pipelineSourceDriver));
  $: loaderOptions = availableConnectors('loader').filter((connector) => connectorMatchesDriver(connector, pipelineDestinationDriver));
  $: enabledPipelines = pipelines.filter((pipeline) => pipeline.enabled).length;
  $: completedRuns = runs.filter((item) => ['succeeded', 'failed', 'timed_out', 'lost'].includes(item.status));
  $: successRate = completedRuns.length
    ? Math.round((successRuns.length / completedRuns.length) * 100)
    : null;
  $: updateAgeSeconds = lastUpdatedAt ? Math.max(0, Math.round((Date.now() - lastUpdatedAt.getTime()) / 1000)) : null;
  $: updatedLabel = updateAgeSeconds === null
    ? 'Waiting for data'
    : updateAgeSeconds < 45
      ? 'Updated just now'
      : `Updated ${Math.max(1, Math.round(updateAgeSeconds / 60))}m ago`;
  $: latestRunByPipeline = runs.reduce((latest, run) => {
    const current = latest[run.pipeline_id];
    if (!current || new Date(run.created_at).getTime() > new Date(current.created_at).getTime()) {
      latest[run.pipeline_id] = run;
    }
    return latest;
  }, {});
  $: filteredPipelines = pipelines.filter((pipeline) => {
    const query = pipelineSearch.trim().toLowerCase();
    const latestStatus = latestRunByPipeline[pipeline.id]?.status;
    const searchable = [
      pipeline.name,
      pipeline.extractor,
      pipeline.loader,
      ...(pipeline.source_filter?.streams || []),
      pipeline.source_filter?.row_filter,
      connectionName(pipeline.source_connection_id),
      connectionName(pipeline.destination_connection_id)
    ].join(' ').toLowerCase();
    const matchesQuery = !query || searchable.includes(query);
    const matchesFilter = pipelineFilter === 'all'
      || (pipelineFilter === 'enabled' && pipeline.enabled)
      || (pipelineFilter === 'disabled' && !pipeline.enabled)
      || (pipelineFilter === 'attention' && ['failed', 'timed_out', 'lost'].includes(latestStatus))
      || (pipelineFilter === 'no_runs' && !latestStatus);
    return matchesQuery && matchesFilter;
  });
  $: filteredSourceConnections = sourceConnections.filter((connection) => {
    const query = connectionSearch.trim().toLowerCase();
    const searchable = [
      connection.name,
      connection.driver,
      driverLabels[connection.driver],
      connection.endpoint?.host,
      connection.endpoint?.database
    ].filter(Boolean).join(' ').toLowerCase();
    return (!query || searchable.includes(query))
      && (connectionFilter === 'all' || connection.driver === connectionFilter);
  });
  $: filteredDestinationConnections = destinationConnections.filter((connection) => {
    const query = connectionSearch.trim().toLowerCase();
    const searchable = [
      connection.name,
      connection.driver,
      driverLabels[connection.driver],
      connection.endpoint?.host,
      connection.endpoint?.database
    ].filter(Boolean).join(' ').toLowerCase();
    return (!query || searchable.includes(query))
      && (connectionFilter === 'all' || connection.driver === connectionFilter);
  });
  $: filteredConnectors = connectors.filter((connector) => {
    const query = connectorSearch.trim().toLowerCase();
    const searchable = [
      connector.name,
      connector.label,
      connector.kind,
      connector.driver,
      driverLabels[connector.driver]
    ].filter(Boolean).join(' ').toLowerCase();
    return (!query || searchable.includes(query))
      && (connectorFilter === 'all' || connector.kind === connectorFilter);
  });
  $: pipelineFiltersActive = Boolean(pipelineSearch.trim()) || pipelineFilter !== 'all';
  $: connectionFiltersActive = Boolean(connectionSearch.trim()) || connectionFilter !== 'all';
  $: connectorFiltersActive = Boolean(connectorSearch.trim()) || connectorFilter !== 'all';
  $: readinessItems = [
    {
      label: 'Source connection',
      detail: sourceConnections.length ? `${sourceConnections.length} configured` : 'Add a system to read from',
      complete: sourceConnections.length > 0
    },
    {
      label: 'Destination connection',
      detail: destinationConnections.length ? `${destinationConnections.length} configured` : 'Add a system to write to',
      complete: destinationConnections.length > 0
    },
    {
      label: 'Pipeline',
      detail: pipelines.length ? `${pipelines.length} configured` : 'Define a data movement',
      complete: pipelines.length > 0
    },
    {
      label: 'Connector catalog',
      detail: connectors.length ? `${connectors.length} available` : 'Sync available connectors',
      complete: connectors.length > 0
    }
  ];
  $: nextReadinessItem = readinessItems.find((item) => !item.complete);
  $: connectionReady = connectionCanSave(connectionForm);

  onMount(() => {
    const onHashChange = () => { page = pageFromHash(); };
    const onAuthRequired = () => {
      authenticated = false;
      currentUser = null;
      loginError = 'Your session has expired. Sign in again.';
    };
    const executionPoller = setInterval(() => {
      if (
        authenticated
        && page === 'Overview'
        && executionRunId
        && executionDetail
        && ['queued', 'running', 'cancelling'].includes(executionDetail.run?.status)
      ) {
        loadExecutionDetail(executionRunId, true);
      }
    }, 3000);
    window.addEventListener('hashchange', onHashChange);
    window.addEventListener('auth:required', onAuthRequired);
    initializeAuth();
    return () => {
      clearInterval(executionPoller);
      window.removeEventListener('hashchange', onHashChange);
      window.removeEventListener('auth:required', onAuthRequired);
    };
  });

  async function initializeAuth() {
    authLoading = true;
    loginError = '';
    try {
      const response = await api.me();
      currentUser = response.user;
      authenticated = true;
      if (currentUser.role !== 'superadmin' && page === 'Users') {
        goToPage('Overview');
      }
      await load();
      if (currentUser.role === 'superadmin') {
        await loadUsers();
      }
    } catch (err) {
      authenticated = false;
      currentUser = null;
      if (err.status !== 401) {
        loginError = err.message;
      }
    } finally {
      authLoading = false;
    }
  }

  async function submitLogin() {
    if (loginBusy) return;
    loginBusy = true;
    loginError = '';
    try {
      const response = await api.login(loginUsername.trim(), loginPassword);
      currentUser = response.user;
      authenticated = true;
      loginPassword = '';
      if (currentUser.role === 'superadmin') {
        await loadUsers();
      }
      await load();
    } catch (err) {
      loginError = err.message;
    } finally {
      loginBusy = false;
    }
  }

  async function logout() {
    try {
      await api.logout();
    } catch {
      // Cookie remains the source of truth; clear local state regardless.
    }
    authenticated = false;
    currentUser = null;
    users = [];
    loginPassword = '';
  }

  async function loadUsers() {
    if (currentUser?.role !== 'superadmin' || usersLoading) return;
    usersLoading = true;
    try {
      const response = await api.users();
      users = response.data || [];
    } catch (err) {
      if (err.status !== 401) error = err.message;
    } finally {
      usersLoading = false;
    }
  }

  function pageFromHash() {
    const match = nav.find((item) => item.name.toLowerCase() === window.location.hash.replace('#', '').toLowerCase());
    return match?.name || 'Overview';
  }

  function goToPage(name) {
    page = name;
    if (name === 'Users') {
      loadUsers();
    }
    const hash = name.toLowerCase();
    if (window.location.hash !== `#${hash}`) {
      window.history.replaceState(null, '', `#${hash}`);
    }
  }

  function availableConnectors(kind) {
    const matching = connectors.filter((item) => item.kind === kind);
    const installed = matching.filter((item) => item.installed !== false);
    return installed.length ? installed : matching;
  }

  function selectedConnectionDriver(id) {
    return connections.find((connection) => connection.id === id)?.driver || '';
  }

  function connectorMatchesDriver(connector, driver) {
    if (!driver) return true;
    if (connector.driver === driver) return true;
    return !connector.driver || !driverLabels[connector.driver];
  }

  function compatibleConnectors(kind, connectionId) {
    const driver = selectedConnectionDriver(connectionId);
    return availableConnectors(kind).filter((connector) => connectorMatchesDriver(connector, driver));
  }

  function handlePipelineFormChange(event) {
    const value = event.target.value;
    if (sourceConnections.some((connection) => connection.id === value)) {
      pipelineForm.source_connection_id = value;
      const options = compatibleConnectors('extractor', value);
      if (!options.some((connector) => connector.name === pipelineForm.extractor)) {
        pipelineForm.extractor = options[0]?.name || '';
      }
      return;
    }
    if (destinationConnections.some((connection) => connection.id === value)) {
      pipelineForm.destination_connection_id = value;
      const options = compatibleConnectors('loader', value);
      if (!options.some((connector) => connector.name === pipelineForm.loader)) {
        pipelineForm.loader = options[0]?.name || '';
      }
    }
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const tenantResponse = await api.tenants();
      tenants = tenantResponse.data || [];
      const previousId = selectedTenant?.id;
      selectedTenant = tenants.find((tenant) => tenant.id === previousId) || tenants[0] || null;
      await loadTenantData();
    } catch (err) {
      error = err.message;
    } finally {
      loading = false;
    }
  }

  async function loadTenantData() {
    if (!selectedTenant) {
      connections = [];
      pipelines = [];
      runs = [];
      connectors = [];
      executionRunId = '';
      executionDetail = null;
      executionError = '';
      return;
    }
    const [connectionResponse, pipelineResponse, runResponse] = await Promise.all([
      api.connections(selectedTenant.id),
      api.pipelines(selectedTenant.id),
      api.runs()
    ]);
    connections = connectionResponse.data || [];
    pipelines = pipelineResponse.data || [];
    const pipelineIds = new Set(pipelines.map((pipeline) => pipeline.id));
    runs = (runResponse.data || []).filter((run) => pipelineIds.has(run.pipeline_id));

    await refreshConnectors();
    lastUpdatedAt = new Date();
    const runId = runs.some((run) => run.id === executionRunId) ? executionRunId : runs[0]?.id || '';
    await loadExecutionDetail(runId, true);
  }

  async function refreshData() {
    if (refreshing) return;
    refreshing = true;
    error = '';
    try {
      await loadTenantData();
      notify('Workspace data refreshed');
    } catch (err) {
      error = err.message;
    } finally {
      refreshing = false;
    }
  }

  async function loadExecutionDetail(runId, silent = false) {
    if (!runId) {
      executionRunId = '';
      executionDetail = null;
      executionError = '';
      return;
    }
    executionRunId = runId;
    if (!silent) executionLoading = true;
    executionError = '';
    try {
      const detail = await api.runLogs(runId);
      if (executionRunId === runId) {
        executionDetail = detail;
        runs = runs.map((run) => run.id === detail.run.id ? detail.run : run);
      }
    } catch (err) {
      if (executionRunId === runId) {
        executionError = err.message;
      }
    } finally {
      if (executionRunId === runId) {
        executionLoading = false;
      }
    }
  }

  function selectExecutionRun(run) {
    if (run.id === executionRunId) return;
    executionDetail = null;
    loadExecutionDetail(run.id);
  }

  async function refreshConnectors() {
    try {
      const connectorResponse = await api.connectors(selectedTenant?.id || '');
      connectors = connectorResponse.data || [];
      connectorSource = connectorResponse.source || 'tenant';
    } catch {
      const connectorResponse = await api.connectors();
      connectors = connectorResponse.data || [];
      connectorSource = 'catalog';
    }
  }

  async function selectTenant(event) {
    selectedTenant = tenants.find((tenant) => tenant.id === event.currentTarget.value) || null;
    await loadTenantData();
  }

  function openNewTenant() {
    editingTenant = null;
    tenantForm = blankTenant();
    showTenantModal = true;
  }

  function openEditTenant() {
    if (!selectedTenant) return;
    editingTenant = selectedTenant;
    tenantForm = {
      id: selectedTenant.id,
      name: selectedTenant.name,
      display_name: selectedTenant.display_name || '',
      environment_name: selectedTenant.environment_name || ''
    };
    showTenantModal = true;
  }

  async function saveTenant() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      if (editingTenant) {
        const updated = await api.updateTenant(editingTenant.id, {
          display_name: tenantForm.display_name,
          environment_name: tenantForm.environment_name || tenantForm.name
        });
        tenants = tenants.map((tenant) => tenant.id === updated.id ? updated : tenant);
        selectedTenant = updated;
        notify('Workspace updated');
      } else {
        const created = await api.createTenant({
          name: tenantForm.name,
          display_name: tenantForm.display_name,
          environment_name: tenantForm.environment_name || tenantForm.name
        });
        tenants = [...tenants, created];
        selectedTenant = created;
        notify('Workspace created');
      }
      showTenantModal = false;
      await loadTenantData();
    } catch (err) {
      error = err.message;
    } finally {
      busy = false;
    }
  }

  function openNewConnection(kind = 'source') {
    editingConnection = null;
    connectionForm = blankConnection(kind);
    connectionStatus = '';
    showConnectionModal = true;
  }

  function openEditConnection(connection) {
    editingConnection = connection;
    connectionForm = {
      id: connection.id,
      name: connection.name,
      kind: connection.kind,
      driver: connection.driver,
      host: connection.endpoint?.host || '',
      port: connection.endpoint?.port || defaultPort(connection.driver),
      database: connection.endpoint?.database || '',
      schema: connection.endpoint?.schema || (connection.driver === 'mongodb' ? '' : 'public'),
      service_name: connection.endpoint?.service_name || '',
      sid: connection.endpoint?.sid || '',
      sslmode: connection.endpoint?.sslmode || (connection.driver === 'mongodb' ? 'disable' : 'prefer'),
      auth_source: connection.endpoint?.auth_source || 'admin',
      username: connection.username || '',
      secret_ref: connection.secret_ref || '',
      password: '',
      load_method: connection.options?.load_method || 'upsert',
      api_url: connection.endpoint?.api_url || '',
      api_path: connection.endpoint?.path || '',
      stream_name: connection.endpoint?.stream_name || '',
      rest_primary_keys: (connection.endpoint?.primary_keys || []).join(', '),
      rest_records_path: connection.endpoint?.records_path || '$[*]',
      auth_method: connection.endpoint?.auth_method || 'no_auth',
      api_key_header: connection.endpoint?.api_key_header || 'Authorization',
      sheet_id: connection.endpoint?.sheet_id || '',
      child_sheet_name: connection.endpoint?.child_sheet_name || '',
      sheet_range: connection.endpoint?.range || '',
      csv_path: connection.endpoint?.path || '',
      csv_entity: connection.endpoint?.entity || '',
      csv_keys: (connection.endpoint?.keys || []).join(', '),
      csv_encoding: connection.endpoint?.encoding || 'utf-8',
      s3_bucket: connection.endpoint?.bucket || '',
      s3_region: connection.endpoint?.region || '',
      s3_prefix: connection.endpoint?.prefix || '',
      s3_format: connection.endpoint?.format || 'parquet',
      s3_endpoint: connection.endpoint?.endpoint_override || '',
      duckdb_path: connection.endpoint?.path || '',
      duckdb_schema: connection.endpoint?.schema || 'main'
    };
    connectionStatus = '';
    showConnectionModal = true;
  }

  function updateDriverDefaults() {
    if (['rest', 'csv', 'google-sheets'].includes(connectionForm.driver)) {
      connectionForm.kind = 'source';
    }
    if (['s3', 'duckdb'].includes(connectionForm.driver)) {
      connectionForm.kind = 'destination';
    }
    connectionForm.port = defaultPort(connectionForm.driver);
    connectionForm.service_name = '';
    connectionForm.sid = '';
    connectionForm.schema = {
      postgres: 'public',
      mysql: '',
      mssql: 'dbo',
      oracle: '',
      mongodb: '',
      rest: '',
      csv: '',
      'google-sheets': '',
      s3: '',
      duckdb: ''
    }[connectionForm.driver] || '';
    connectionForm.sslmode = {
      postgres: 'prefer',
      mysql: 'prefer',
      mssql: 'prefer',
      oracle: '',
      mongodb: 'disable',
      rest: 'require',
      csv: '',
      'google-sheets': 'require',
      s3: 'require',
      duckdb: ''
    }[connectionForm.driver] || '';
    if (connectionForm.driver === 'mongodb' && !connectionForm.auth_source) {
      connectionForm.auth_source = 'admin';
    }
  }

  function defaultPort(driver) {
    return {
      postgres: '5432',
      mysql: '3306',
      mssql: '1433',
      oracle: '1521',
      mongodb: '27017',
      rest: '443',
      'google-sheets': '443',
      s3: '443'
    }[driver] || '';
  }

  function isDatabaseDriver(driver) {
    return ['postgres', 'mysql', 'mssql', 'oracle', 'mongodb'].includes(driver);
  }

  function connectionCanSave(form) {
    if (!form.name.trim()) return false;
    if (isDatabaseDriver(form.driver)) {
      return Boolean(form.host.trim() && form.database.trim() && (form.secret_ref.trim() || form.password));
    }
    if (form.driver === 'rest') {
      if (!form.api_url.trim() || !form.stream_name.trim()) return false;
      if (form.auth_method === 'no_auth') return true;
      if (form.auth_method === 'basic') return Boolean(form.username.trim() && form.secret_ref.trim());
      return Boolean(form.secret_ref.trim());
    }
    if (form.driver === 'csv') return Boolean(form.csv_path.trim() && form.csv_entity.trim());
    if (form.driver === 'google-sheets') return Boolean(form.sheet_id.trim() && form.secret_ref.trim());
    if (form.driver === 's3') return Boolean(form.s3_bucket.trim() && form.s3_region.trim() && form.s3_format.trim());
    if (form.driver === 'duckdb') return Boolean(form.duckdb_path.trim());
    return false;
  }

  function connectionEndpoint() {
    const form = connectionForm;
    if (form.driver === 'rest') {
      return {
        api_url: form.api_url.trim(),
        path: form.api_path.trim(),
        stream_name: form.stream_name.trim(),
        primary_keys: parseStreamSelection(form.rest_primary_keys),
        records_path: form.rest_records_path.trim() || '$[*]',
        auth_method: form.auth_method,
        api_key_header: form.api_key_header.trim()
      };
    }
    if (form.driver === 'csv') {
      return {
        path: form.csv_path.trim(),
        entity: form.csv_entity.trim(),
        keys: parseStreamSelection(form.csv_keys),
        encoding: form.csv_encoding.trim()
      };
    }
    if (form.driver === 'google-sheets') {
      return {
        sheet_id: form.sheet_id.trim(),
        child_sheet_name: form.child_sheet_name.trim(),
        range: form.sheet_range.trim()
      };
    }
    if (form.driver === 's3') {
      return {
        bucket: form.s3_bucket.trim(),
        region: form.s3_region.trim(),
        prefix: form.s3_prefix.trim(),
        format: form.s3_format,
        endpoint_override: form.s3_endpoint.trim()
      };
    }
    if (form.driver === 'duckdb') {
      return { path: form.duckdb_path.trim(), schema: form.duckdb_schema.trim() || 'main' };
    }
    const endpoint = {
      host: form.host.trim(),
      port: form.port,
      database: form.database.trim()
    };
    if (form.schema) endpoint.schema = form.schema;
    if (form.service_name) endpoint.service_name = form.service_name;
    if (form.sid) endpoint.sid = form.sid;
    if (form.sslmode) endpoint.sslmode = form.sslmode;
    if (form.driver === 'mongodb' && form.auth_source) endpoint.auth_source = form.auth_source;
    return endpoint;
  }

  function parseStreamSelection(value) {
    return [...new Set(
      String(value || '')
        .split(/[\n,]/)
        .map((item) => item.trim())
        .filter(Boolean)
    )];
  }

  async function saveConnection() {
    if (connectionSaving || !selectedTenant) return;
    if (!connectionCanSave(connectionForm)) {
      connectionStatus = 'Complete the required fields for this connection type.';
      return;
    }
    connectionSaving = true;
    error = '';
    try {
      const endpoint = connectionEndpoint();

      if (isDatabaseDriver(connectionForm.driver) && connectionForm.password) {
        connectionStatus = 'Testing database authentication…';
        await api.testConnection({
          driver: connectionForm.driver,
          endpoint,
          username: connectionForm.username,
          password: connectionForm.password
        });
      }

      const payload = {
        name: connectionForm.name,
        kind: connectionForm.kind,
        driver: connectionForm.driver,
        endpoint,
        username: connectionForm.username,
        options: connectionForm.kind === 'destination'
          ? (['postgres', 'mssql'].includes(connectionForm.driver)
              ? { load_method: connectionForm.load_method }
              : {})
          : {}
      };
      if (connectionForm.secret_ref) payload.secret_ref = connectionForm.secret_ref;
      if (connectionForm.password) payload.password = connectionForm.password;

      connectionStatus = 'Saving connection…';
      if (editingConnection) {
        await api.updateConnection(editingConnection.id, payload);
        notify('Connection updated');
      } else {
        await api.createConnection({ ...payload, tenant_id: selectedTenant.id });
        notify('Connection added');
      }
      showConnectionModal = false;
      await loadTenantData();
    } catch (err) {
      connectionStatus = err.message;
      error = err.message;
    } finally {
      connectionSaving = false;
    }
  }

  async function openNewPipeline() {
    if (!connectors.length) {
      await refreshConnectors();
    }
    if (!availableConnectors('extractor').length || !availableConnectors('loader').length) {
      error = 'No extractor/loader connectors are available. Provision the tenant plugins first.';
      return;
    }
    const sourceId = sourceConnections[0]?.id || '';
    const destinationId = destinationConnections[0]?.id || '';
    const extractors = compatibleConnectors('extractor', sourceId);
    const loaders = compatibleConnectors('loader', destinationId);
    editingPipeline = null;
    pipelineForm = blankPipeline();
    pipelineForm.source_connection_id = sourceId;
    pipelineForm.destination_connection_id = destinationId;
    pipelineForm.extractor = extractors[0]?.name || '';
    pipelineForm.loader = loaders[0]?.name || '';
    pipelineStatus = '';
    showPipelineModal = true;
  }

  function openEditPipeline(pipeline) {
    editingPipeline = pipeline;
    pipelineForm = {
      id: pipeline.id,
      name: pipeline.name,
      extractor: pipeline.extractor,
      loader: pipeline.loader,
      source_connection_id: pipeline.source_connection_id,
      destination_connection_id: pipeline.destination_connection_id,
      state_id_suffix: pipeline.state_id_suffix || '',
      default_timeout_seconds: pipeline.default_timeout_seconds,
      max_attempts: pipeline.max_attempts,
      enabled: pipeline.enabled,
      source_streams: (pipeline.source_filter?.streams || []).join('\n'),
      row_filter: pipeline.source_filter?.row_filter || '',
      transfer_batch_size: String(pipeline.transfer_options?.batch_size_rows || '50000'),
      transfer_validate_records: pipeline.transfer_options?.validate_records !== false,
      transfer_load_method: pipeline.transfer_options?.load_method || ''
    };
    pipelineStatus = '';
    showPipelineModal = true;
  }

  async function savePipeline() {
    if (busy || !selectedTenant) return;
    const extractor = connectors.find((connector) => connector.name === pipelineForm.extractor);
    const loader = connectors.find((connector) => connector.name === pipelineForm.loader);
    if (!pipelineForm.source_connection_id || !pipelineForm.destination_connection_id) {
      pipelineStatus = 'Select both a source and destination connection.';
      return;
    }
    if (!extractor || !connectorMatchesDriver(extractor, selectedConnectionDriver(pipelineForm.source_connection_id))) {
      pipelineStatus = `Choose an extractor compatible with the selected ${driverLabels[selectedConnectionDriver(pipelineForm.source_connection_id)] || 'source'} connection.`;
      return;
    }
    if (!loader || !connectorMatchesDriver(loader, selectedConnectionDriver(pipelineForm.destination_connection_id))) {
      pipelineStatus = `Choose a loader compatible with the selected ${driverLabels[selectedConnectionDriver(pipelineForm.destination_connection_id)] || 'destination'} connection.`;
      return;
    }
    busy = true;
    error = '';
    pipelineStatus = '';
    try {
      const payload = {
        name: pipelineForm.name,
        extractor: pipelineForm.extractor,
        loader: pipelineForm.loader,
        source_connection_id: pipelineForm.source_connection_id,
        destination_connection_id: pipelineForm.destination_connection_id,
        state_id_suffix: pipelineForm.state_id_suffix,
        default_timeout_seconds: Number(pipelineForm.default_timeout_seconds),
        max_attempts: Number(pipelineForm.max_attempts),
        enabled: pipelineForm.enabled,
        source_filter: {
          streams: parseStreamSelection(pipelineForm.source_streams),
          row_filter: String(pipelineForm.row_filter || '').trim()
        },
        transfer_options: pipelineForm.loader === 'target-postgres'
          ? {
              batch_size_rows: Number(pipelineForm.transfer_batch_size) || 50000,
              validate_records: pipelineForm.transfer_validate_records,
              load_method: pipelineForm.transfer_load_method
            }
          : {
              batch_size_rows: 0,
              validate_records: null,
              load_method: ''
            }
      };
      if (editingPipeline) {
        await api.updatePipeline(editingPipeline.id, payload);
        notify('Pipeline updated');
      } else {
        await api.createPipeline({ ...payload, tenant_id: selectedTenant.id });
        notify('Pipeline created');
      }
      showPipelineModal = false;
      goToPage('Pipelines');
      await loadTenantData();
    } catch (err) {
      error = err.message;
      pipelineStatus = err.message;
    } finally {
      busy = false;
    }
  }

  async function runPipeline(pipeline) {
    try {
      const run = await api.run(pipeline.id);
      await loadTenantData();
      await loadExecutionDetail(run.id);
      notify(`${pipeline.name} queued`);
    } catch (err) {
      error = err.message;
    }
  }

  function requestStopRun(run) {
    if (!run?.id) return;
    stopTarget = run;
  }

  async function confirmStopRun() {
    if (!stopTarget || stopBusy) return;
    stopBusy = true;
    error = '';
    try {
      await api.cancelRun(stopTarget.id);
      const stoppedId = stopTarget.id;
      stopTarget = null;
      await loadTenantData();
      await loadExecutionDetail(stoppedId, true);
      notify('Stop requested');
    } catch (err) {
      error = err.message;
    } finally {
      stopBusy = false;
    }
  }

  function requestDelete(type, item) {
    const copy = {
      tenant: {
        title: 'Delete workspace?',
        message: `Delete “${item.display_name || item.name}” from the data integration platform?`,
        detail: 'Connections and pipeline history for this workspace are also removed. Meltano project files on disk are left untouched.'
      },
      connection: {
        title: 'Delete connection?',
        message: `Delete “${item.name}” from this workspace?`,
        detail: 'A connection cannot be deleted while a pipeline still references it.'
      },
      pipeline: {
        title: 'Delete pipeline?',
        message: `Delete “${item.name}”?`,
        detail: 'Run history is removed with the pipeline. Deletion is blocked while a run is active.'
      },
      user: {
        title: 'Delete user?',
        message: `Delete dashboard access for "${item.username}"?`,
        detail: 'Existing sessions for this user are revoked immediately.'
      }
    }[type];
    deleteTarget = { type, item, ...copy };
  }

  async function confirmDelete() {
    if (!deleteTarget || deleteBusy) return;
    deleteBusy = true;
    error = '';
    const deletedType = deleteTarget.type;
    try {
      if (deleteTarget.type === 'tenant') {
        await api.deleteTenant(deleteTarget.item.id);
        tenants = tenants.filter((tenant) => tenant.id !== deleteTarget.item.id);
        selectedTenant = tenants[0] || null;
        notify('Workspace deleted');
      } else if (deleteTarget.type === 'connection') {
        await api.deleteConnection(deleteTarget.item.id);
        notify('Connection deleted');
      } else if (deleteTarget.type === 'user') {
        await api.deleteUser(deleteTarget.item.id);
        notify('User deleted');
      } else {
        await api.deletePipeline(deleteTarget.item.id);
        notify('Pipeline deleted');
      }
      deleteTarget = null;
      if (deletedType === 'user') {
        await loadUsers();
      } else {
        await loadTenantData();
      }
    } catch (err) {
      error = err.message;
    } finally {
      deleteBusy = false;
    }
  }

  function openNewUser() {
    editingUser = null;
    userForm = { username: '', display_name: '', password: '', role: 'admin' };
    userStatus = '';
    showUserModal = true;
  }

  function openEditUser(user) {
    editingUser = user;
    userForm = {
      username: user.username,
      display_name: user.display_name || '',
      password: '',
      role: user.role
    };
    userStatus = '';
    showUserModal = true;
  }

  async function saveUser() {
    if (userSaving) return;
    userSaving = true;
    userStatus = '';
    try {
      const payload = {
        username: userForm.username.trim().toLowerCase(),
        display_name: userForm.display_name.trim(),
        role: userForm.role
      };
      if (userForm.password) payload.password = userForm.password;
      if (editingUser) {
        await api.updateUser(editingUser.id, payload);
        notify('User updated');
      } else {
        await api.createUser(payload);
        notify('User created');
      }
      showUserModal = false;
      await loadUsers();
    } catch (err) {
      userStatus = err.message;
    } finally {
      userSaving = false;
    }
  }

  function openPasswordModal() {
    passwordForm = { current_password: '', new_password: '', confirm_password: '' };
    passwordStatus = '';
    showPasswordModal = true;
  }

  async function savePassword() {
    if (passwordSaving) return;
    if (passwordForm.new_password !== passwordForm.confirm_password) {
      passwordStatus = 'New password confirmation does not match.';
      return;
    }
    passwordSaving = true;
    passwordStatus = '';
    try {
      await api.changePassword(passwordForm.current_password, passwordForm.new_password);
      showPasswordModal = false;
      notify('Password updated');
    } catch (err) {
      passwordStatus = err.message;
    } finally {
      passwordSaving = false;
    }
  }

  function notify(message) {
    toast = message;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast = ''; }, 3200);
  }

  function timeAgo(value) {
    if (!value) return '—';
    const minutes = Math.max(1, Math.round((Date.now() - new Date(value).getTime()) / 60000));
    if (minutes < 60) return `${minutes}m ago`;
    if (minutes < 1440) return `${Math.round(minutes / 60)}h ago`;
    return `${Math.round(minutes / 1440)}d ago`;
  }

  function formatRunTime(value) {
    if (!value) return 'No runs';
    return new Intl.DateTimeFormat('en', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    }).format(new Date(value));
  }

  function formatNumber(value) {
    return value === null || value === undefined ? '—' : new Intl.NumberFormat('en-US').format(value);
  }

  function executionDuration(run) {
    if (!run?.started_at) return '—';
    const end = run.ended_at ? new Date(run.ended_at).getTime() : Date.now();
    const seconds = Math.max(0, Math.round((end - new Date(run.started_at).getTime()) / 1000));
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    return `${minutes}m ${seconds % 60}s`;
  }

  function formatLogTime(value) {
    if (!value) return '';
    return new Intl.DateTimeFormat('en-GB', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false
    }).format(new Date(value));
  }

  function stepStyle(status) {
    return {
      completed: 'border-emerald-200 bg-emerald-50 text-emerald-700',
      active: 'border-blue-200 bg-blue-50 text-brand',
      failed: 'border-rose-200 bg-rose-50 text-rose-700',
      cancelled: 'border-slate-200 bg-slate-100 text-slate-600',
      pending: 'border-line bg-white text-slate-400'
    }[status] || 'border-line bg-white text-slate-400';
  }

  function statusStyle(status) {
    return statusStyles[status] || 'bg-slate-100 text-slate-600';
  }

  function statusLabel(status) {
    return String(status || 'unknown').replaceAll('_', ' ');
  }

  function connectionName(id) {
    return connections.find((connection) => connection.id === id)?.name || 'Unknown';
  }

  function connectionDriver(id) {
    return connections.find((connection) => connection.id === id)?.driver || 'postgres';
  }

  function pipelineName(id) {
    return pipelines.find((pipeline) => pipeline.id === id)?.name || 'Pipeline';
  }
</script>

{#if authLoading}
  <div class="grid min-h-screen place-items-center bg-navy text-sm text-slate-400">
    <div class="text-center">
      <span class="mx-auto grid h-11 w-11 place-items-center rounded-xl bg-brand text-white"><Icon name="workflow" size={22} /></span>
      <p class="mt-4 font-medium">Checking your session...</p>
    </div>
  </div>
{:else if !authenticated}
  <div class="grid min-h-screen place-items-center bg-[radial-gradient(circle_at_top_left,_#1d4ed8_0,_#0f172a_38%,_#08101f_100%)] p-5">
    <div class="w-full max-w-md rounded-2xl border border-white/10 bg-white p-7 shadow-2xl sm:p-9">
      <div class="mb-7 flex items-center gap-3">
        <span class="grid h-11 w-11 place-items-center rounded-xl bg-brand text-white"><Icon name="workflow" size={22} /></span>
        <div>
          <h1 class="display text-xl font-semibold text-ink">Data Integration Platform</h1>
          <p class="mt-1 text-xs text-slate-500">Sign in to manage pipelines and access.</p>
        </div>
      </div>
      <form on:submit|preventDefault={submitLogin}>
        <label class="field-label">Username<input bind:value={loginUsername} autocomplete="username" placeholder="superadmin" /></label>
        <label class="field-label">Password<input type="password" bind:value={loginPassword} autocomplete="current-password" placeholder="Enter your password" /></label>
        {#if loginError}<div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3.5 py-2.5 text-xs leading-5 text-rose-700">{loginError}</div>{/if}
        <button type="submit" class="button-primary w-full" disabled={loginBusy || !loginUsername.trim() || !loginPassword}>
          {loginBusy ? 'Signing in...' : 'Sign in'}
        </button>
      </form>
    </div>
  </div>
{:else}
<div class="flex min-h-screen">
  <aside class="sidebar hidden w-[252px] shrink-0 flex-col border-r border-slate-800 px-3 py-4 lg:flex">
    <div class="mb-7 flex items-start gap-3 px-2">
      <div class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-brand text-white">
        <Icon name="workflow" size={20} strokeWidth={2} />
      </div>
      <div class="min-w-0 pt-0.5">
        <div class="text-[13px] font-semibold leading-[18px] tracking-tight text-white">Data Integration<br />Platform</div>
        <div class="mt-1 text-[10px] font-medium uppercase tracking-[.16em] text-slate-500">Integration control</div>
      </div>
    </div>

    <div class="mb-6 rounded-lg border border-white/[.08] bg-white/[.035] p-3">
      <div class="mb-2 flex items-center justify-between">
        <span class="text-[10px] font-semibold uppercase tracking-[.16em] text-slate-500">Workspace</span>
        {#if selectedTenant}
          <div class="flex gap-0.5">
            <button class="grid h-6 w-6 place-items-center rounded text-slate-400 transition hover:bg-white/10 hover:text-white" title="Edit workspace" on:click={openEditTenant}>
              <Icon name="edit" size={13} />
            </button>
            <button class="grid h-6 w-6 place-items-center rounded text-slate-400 transition hover:bg-rose-500/15 hover:text-rose-300" title="Delete workspace" on:click={() => requestDelete('tenant', selectedTenant)}>
              <Icon name="trash" size={13} />
            </button>
          </div>
        {/if}
      </div>

      <div class="relative">
        <select class="w-full appearance-none rounded-md border border-white/10 bg-navy px-2.5 py-2 pr-8 text-xs font-semibold text-white outline-none transition focus:border-brand" on:change={selectTenant} disabled={!tenants.length}>
          {#if !selectedTenant}<option>No workspace</option>{/if}
          {#each tenants as tenant}
            <option class="text-slate-900" value={tenant.id} selected={selectedTenant?.id === tenant.id}>{tenant.display_name || tenant.name}</option>
          {/each}
        </select>
        <span class="pointer-events-none absolute right-2.5 top-2 text-slate-500"><Icon name="chevron" size={14} /></span>
      </div>
      {#if selectedTenant}
        <p class="mt-2 truncate text-[10px] text-slate-500">Environment: <span class="text-slate-400">{selectedTenant.environment_name || 'default'}</span></p>
      {/if}
    </div>

    <div class="mb-2 px-3 text-[10px] font-semibold uppercase tracking-[.16em] text-slate-600">Manage</div>
    <nav class="space-y-1" aria-label="Primary navigation">
      {#each visibleNav as item}
        <button class:active-nav={page === item.name} class="nav-item" aria-current={page === item.name ? 'page' : undefined} on:click={() => goToPage(item.name)}>
          <span class="nav-icon"><Icon name={item.icon} size={16} /></span>
          <span>{item.name}</span>
          {#if item.name === 'Pipelines'}<span class="ml-auto rounded bg-white/[.06] px-1.5 py-0.5 text-[10px] text-slate-400">{pipelines.length}</span>{/if}
        </button>
      {/each}
    </nav>

    <div class="mt-auto border-t border-white/[.07] px-2 pt-4">
      <div class="mb-4 rounded-lg bg-white/[.035] p-3">
        <p class="truncate text-xs font-semibold text-white">{currentUser?.display_name || currentUser?.username}</p>
        <p class="mt-1 text-[10px] capitalize text-slate-500">{currentUser?.role}</p>
        <div class="mt-3 flex gap-2">
          <button class="rounded-md border border-white/10 px-2 py-1 text-[10px] font-semibold text-slate-300 hover:bg-white/10" on:click={openPasswordModal}>Password</button>
          <button class="rounded-md border border-white/10 px-2 py-1 text-[10px] font-semibold text-rose-300 hover:bg-rose-500/10" on:click={logout}>Sign out</button>
        </div>
      </div>
      <div class="flex items-center justify-between">
        <span class="text-[11px] font-medium text-slate-400">Platform status</span>
        <span class="inline-flex items-center gap-1.5 text-[10px] font-semibold text-emerald-400">
          <span class="h-1.5 w-1.5 rounded-full bg-emerald-400"></span> Operational
        </span>
      </div>
      <p class="mt-2 text-[10px] leading-4 text-slate-600">API and execution services are available.</p>
    </div>
  </aside>

  <main class="min-w-0 flex-1">
    <header class="sticky top-0 z-30 flex min-h-[68px] items-center justify-between gap-4 border-b border-line bg-white px-5 md:px-8">
      <div class="min-w-0">
        <div class="flex items-center gap-1.5 text-[10px] font-medium text-slate-500">
          <span>Data Integration Platform</span><span class="text-slate-300">/</span>
          <span class="truncate text-slate-700">{selectedTenant?.display_name || selectedTenant?.name || 'Getting started'}</span>
        </div>
        <h1 class="display mt-0.5 text-[17px] font-semibold text-ink">{page}</h1>
      </div>
      <div class="flex items-center gap-2">
        {#if selectedTenant}
          <div class="hidden items-center gap-2 pr-2 text-[11px] text-slate-500 xl:flex">
            <span class="h-1.5 w-1.5 rounded-full bg-emerald-500"></span>
            <span>{updatedLabel}</span>
          </div>
          <button class="icon-button hidden border border-line bg-white sm:grid" title="Refresh workspace data" aria-label="Refresh workspace data" disabled={refreshing} on:click={refreshData}>
            <span class:animate-spin={refreshing}><Icon name="refresh" size={15} /></span>
          </button>
        {/if}
        {#if page !== 'Connectors'}
          <button class="hidden items-center gap-2 rounded-lg border border-line bg-white px-3 py-2 text-xs font-semibold text-slate-600 transition hover:bg-slate-50 sm:flex" on:click={() => goToPage('Connectors')}>
            <Icon name="connectors" size={14} /> Connector catalog
          </button>
        {/if}
        {#if selectedTenant && page !== 'Pipelines'}
          <button class="button-primary px-3.5 py-2 text-xs" on:click={openNewPipeline}>
            <Icon name="plus" size={14} /> New pipeline
          </button>
        {/if}
      </div>
    </header>

    <div class="scrollbar min-h-[calc(100vh-68px)] overflow-auto px-5 pb-28 pt-6 md:px-8 lg:pb-8">
      <div class="mx-auto max-w-[1440px]">
        {#if error}
          <div class="mb-6 flex items-start gap-3 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
            <Icon name="warning" size={18} />
            <span class="flex-1">{error}</span>
            <button on:click={() => error = ''} aria-label="Dismiss"><Icon name="x" size={16} /></button>
          </div>
        {/if}

        {#if loading}
          <div class="grid min-h-[420px] place-items-center">
            <div class="text-center">
              <span class="mx-auto mb-4 block h-8 w-8 animate-spin rounded-full border-2 border-slate-200 border-t-brand"></span>
              <p class="text-sm font-medium text-slate-600">Loading platform data...</p>
            </div>
          </div>
        {:else if !selectedTenant}
          <div class="mx-auto mt-16 max-w-xl rounded-xl border border-line bg-white p-10 text-center shadow-panel">
            <span class="mx-auto grid h-12 w-12 place-items-center rounded-lg bg-blue-50 text-brand">
              <Icon name="workflow" size={24} />
            </span>
            <p class="mt-5 text-[10px] font-semibold uppercase tracking-[.16em] text-brand">Get started</p>
            <h2 class="display mt-2 text-2xl font-semibold text-ink">Create your first workspace</h2>
            <p class="mx-auto mt-2 max-w-sm text-sm leading-6 text-slate-500">A workspace groups database connections, integration pipelines, and run history for one environment.</p>
            <button class="button-primary mt-7" on:click={openNewTenant}><Icon name="plus" size={16} /> Create workspace</button>
          </div>
        {:else if page === 'Overview'}
          <section class="mb-6 flex flex-wrap items-start justify-between gap-5">
            <div>
              <p class="eyebrow text-brand">Operations overview</p>
              <h2 class="display mt-1.5 text-2xl font-semibold tracking-tight text-ink">Workspace overview</h2>
              <p class="mt-2 max-w-2xl text-sm leading-6 text-slate-500">Monitor integrations, data movement, and execution health for {selectedTenant.display_name || selectedTenant.name}.</p>
            </div>
            <div class="flex flex-wrap gap-2">
              <button class="button-secondary px-3.5 py-2 text-xs" on:click={() => openNewConnection('source')}>
                <Icon name="plus" size={14} /> Add connection
              </button>
            </div>
          </section>

          <section class="mb-5 flex flex-wrap items-center gap-x-6 gap-y-2 rounded-lg border border-line bg-white px-4 py-3 text-xs text-slate-500 shadow-panel">
            <span class="inline-flex items-center gap-2 font-medium text-slate-700">
              <span class="h-2 w-2 rounded-full bg-emerald-500"></span>
              Platform operational
            </span>
            <span>Environment <strong class="font-semibold text-slate-700">{selectedTenant.environment_name || 'default'}</strong></span>
            <span>{sourceConnections.length} source{sourceConnections.length === 1 ? '' : 's'} and {destinationConnections.length} destination{destinationConnections.length === 1 ? '' : 's'}</span>
            <span class="ml-auto text-slate-400">{updatedLabel}</span>
          </section>

          <section class="mb-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-4" aria-label="Workspace summary">
            <article class="stat-card">
              <div class="flex items-start justify-between">
                <div><p class="text-xs font-medium text-slate-500">Pipelines</p><p class="metric">{pipelines.length}</p><p class="mt-2 text-[11px] text-slate-500">{enabledPipelines} enabled</p></div>
                <span class="grid h-9 w-9 place-items-center rounded-lg bg-blue-50 text-brand"><Icon name="pipelines" size={18} /></span>
              </div>
            </article>
            <article class="stat-card">
              <div class="flex items-start justify-between">
                <div><p class="text-xs font-medium text-slate-500">Connections</p><p class="metric">{connections.length}</p><p class="mt-2 text-[11px] text-slate-500">{sourceConnections.length} sources / {destinationConnections.length} destinations</p></div>
                <span class="grid h-9 w-9 place-items-center rounded-lg bg-slate-100 text-slate-600"><Icon name="connections" size={18} /></span>
              </div>
            </article>
            <article class="stat-card">
              <div class="flex items-start justify-between">
                <div><p class="text-xs font-medium text-slate-500">Active runs</p><p class="metric">{activeRuns.length}</p><p class="mt-2 text-[11px] text-slate-500">{failedRuns.length} need attention</p></div>
                <span class={`grid h-9 w-9 place-items-center rounded-lg ${failedRuns.length ? 'bg-amber-50 text-amber-700' : 'bg-slate-100 text-slate-600'}`}><Icon name="activity" size={18} /></span>
              </div>
            </article>
            <article class="stat-card">
              <div class="flex items-start justify-between">
                <div><p class="text-xs font-medium text-slate-500">Success rate</p><p class="metric">{successRate === null ? '—' : `${successRate}%`}</p><p class="mt-2 text-[11px] text-slate-500">Across {completedRuns.length} completed runs</p></div>
                <span class={`grid h-9 w-9 place-items-center rounded-lg ${successRate === null ? 'bg-slate-100 text-slate-500' : successRate >= 95 ? 'bg-emerald-50 text-emerald-700' : 'bg-amber-50 text-amber-700'}`}><Icon name="check" size={18} /></span>
              </div>
            </article>
          </section>

          <div class="grid gap-5 xl:grid-cols-[1.35fr_1fr]">
            <section class="panel overflow-hidden">
              <div class="flex items-center justify-between border-b border-line px-5 py-4">
                <div><h3 class="text-sm font-semibold text-ink">Recent runs</h3><p class="mt-1 text-xs text-slate-500">Execution history across this workspace</p></div>
                <button class="text-xs font-semibold text-brand hover:text-brand-deep" on:click={() => goToPage('Pipelines')}>View pipelines</button>
              </div>
              {#if runs.length}
                <div class="divide-y divide-line">
                  {#each runs.slice(0, 6) as run (run.id)}
                    <button
                      class={`flex w-full items-center gap-3 px-5 py-3.5 text-left transition hover:bg-slate-50 ${executionRunId === run.id ? 'bg-blue-50/60' : ''}`}
                      title="View execution details"
                      aria-pressed={executionRunId === run.id}
                      on:click={() => selectExecutionRun(run)}
                    >
                      <span class:animate-pulse={run.status === 'running'} class={`grid h-8 w-8 shrink-0 place-items-center rounded-lg ${statusStyle(run.status)}`}><Icon name="activity" size={16} /></span>
                      <div class="min-w-0 flex-1">
                        <p class="truncate text-sm font-semibold text-slate-800">{pipelineName(run.pipeline_id)}</p>
                        <p class="mt-0.5 text-[11px] text-slate-500">{run.trigger} · {timeAgo(run.created_at)}</p>
                      </div>
                      <span class={`status-pill ${statusStyle(run.status)}`}><span class="status-dot"></span>{statusLabel(run.status)}</span>
                      <Icon name="arrow-right" size={14} />
                    </button>
                  {/each}
                </div>
              {:else}
                <div class="px-6 py-14 text-center text-sm text-slate-500">No runs yet. Start a pipeline to populate activity.</div>
              {/if}
            </section>

            <section class="panel overflow-hidden">
              <div class="flex items-center justify-between border-b border-line px-5 py-4">
                <div><h3 class="text-sm font-semibold text-ink">Workspace readiness</h3><p class="mt-1 text-xs text-slate-500">Core configuration checklist</p></div>
                <button class="text-xs font-semibold text-brand hover:text-brand-deep" on:click={() => goToPage('Connections')}>Manage</button>
              </div>
              <div class="border-b border-line bg-slate-50/70 px-5 py-4">
                {#if nextReadinessItem}
                  <p class="text-xs font-semibold text-slate-700">Next recommended step</p>
                  <p class="mt-1 text-xs leading-5 text-slate-500">{nextReadinessItem.label}: {nextReadinessItem.detail}.</p>
                {:else}
                  <p class="text-xs font-semibold text-emerald-700">Core setup complete</p>
                  <p class="mt-1 text-xs leading-5 text-slate-500">Connections, connectors, and pipelines are ready to operate.</p>
                {/if}
              </div>
              <div class="divide-y divide-line px-5">
                {#each readinessItems as item}
                  <div class="flex items-center gap-3 py-3">
                    <span class={`grid h-5 w-5 shrink-0 place-items-center rounded-full ${item.complete ? 'bg-emerald-50 text-emerald-700' : 'border border-slate-300 bg-white text-transparent'}`}>
                      <Icon name="check" size={12} />
                    </span>
                    <span class="min-w-0 flex-1">
                      <span class="block text-xs font-medium text-slate-700">{item.label}</span>
                      <span class="mt-0.5 block text-[10px] text-slate-500">{item.detail}</span>
                    </span>
                  </div>
                {/each}
              </div>
              <div class="border-t border-line px-5 pb-2 pt-3 text-[10px] font-semibold uppercase tracking-[.12em] text-slate-400">Connections</div>
              {#if connections.length}
                <div class="divide-y divide-line">
                  {#each connections.slice(0, 4) as connection (connection.id)}
                    <div class="flex items-center gap-3 px-5 py-3.5">
                      <DatabaseIcon driver={connection.driver} size={28} />
                      <div class="min-w-0 flex-1"><p class="truncate text-sm font-semibold text-slate-800">{connection.name}</p><p class="mt-0.5 text-[11px] text-slate-500">{driverLabels[connection.driver] || connection.driver} · {connection.kind}</p></div>
                      <span class="h-2 w-2 rounded-full bg-emerald-400"></span>
                    </div>
                  {/each}
                </div>
              {:else}
                <div class="px-6 py-14 text-center text-sm text-slate-500">Add a source and destination to begin.</div>
              {/if}
            </section>
          </div>

          {#if runs.length}
            <section class="panel mt-5 overflow-hidden">
              <div class="flex flex-wrap items-center justify-between gap-4 border-b border-line px-5 py-4">
                <div class="min-w-0">
                  <p class="eyebrow text-brand">Execution trace</p>
                  <div class="mt-1.5 flex flex-wrap items-center gap-2">
                    <h3 class="truncate text-sm font-semibold text-ink">{executionDetail ? pipelineName(executionDetail.run.pipeline_id) : 'Latest run'}</h3>
                    {#if executionDetail}
                      <span class={`status-pill ${statusStyle(executionDetail.run.status)}`}><span class="status-dot"></span>{statusLabel(executionDetail.run.status)}</span>
                    {/if}
                  </div>
                  <p class="mt-1 text-[11px] text-slate-500">
                    {#if executionDetail}
                      {executionDetail.run.trigger} run · {formatRunTime(executionDetail.run.created_at)}
                    {:else}
                      Select a run to inspect its progress and log.
                    {/if}
                  </p>
                </div>
                <div class="flex items-center gap-2">
                  {#if executionDetail && ['queued', 'running', 'cancelling'].includes(executionDetail.run.status)}
                    <span class="inline-flex items-center gap-1.5 rounded-full bg-blue-50 px-2.5 py-1 text-[10px] font-semibold text-brand">
                      <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-brand"></span> Live
                    </span>
                    <button
                      class="inline-flex items-center gap-1.5 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs font-semibold text-rose-700 transition hover:border-rose-300 hover:bg-rose-100 disabled:cursor-not-allowed disabled:opacity-60"
                      disabled={executionDetail.run.status === 'cancelling' || stopBusy}
                      on:click={() => requestStopRun(executionDetail.run)}
                    >
                      <Icon name="stop" size={13} />
                      {executionDetail.run.status === 'cancelling' ? 'Stopping...' : 'Stop run'}
                    </button>
                  {/if}
                  <button class="icon-button border border-line bg-white" title="Refresh execution details" aria-label="Refresh execution details" disabled={!executionRunId || executionLoading} on:click={() => loadExecutionDetail(executionRunId)}>
                    <span class:animate-spin={executionLoading}><Icon name="refresh" size={15} /></span>
                  </button>
                </div>
              </div>

              {#if executionLoading && !executionDetail}
                <div class="flex min-h-48 items-center justify-center gap-3 text-sm text-slate-500">
                  <span class="h-5 w-5 animate-spin rounded-full border-2 border-slate-200 border-t-brand"></span>
                  Loading execution detail...
                </div>
              {:else if executionError}
                <div class="m-5 flex items-start gap-3 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                  <Icon name="warning" size={17} />
                  <span>{executionError}</span>
                </div>
              {:else if executionDetail}
                <div class="p-5">
                  <div class="rounded-lg border border-line bg-slate-50/70 p-4">
                    <div class="flex flex-wrap items-center justify-between gap-2">
                      <div>
                        <p class="text-[10px] font-semibold uppercase tracking-[.14em] text-slate-500">Current stage</p>
                        <p class="mt-1 text-sm font-semibold capitalize text-ink">{executionDetail.summary.stage}</p>
                      </div>
                      <span class="text-sm font-semibold text-ink">{executionDetail.summary.progress_percent}%</span>
                    </div>
                    <div class="mt-3 h-2 overflow-hidden rounded-full bg-slate-200">
                      <div
                        class={`h-full rounded-full transition-all ${executionDetail.summary.stage === 'failed' ? 'bg-rose-500' : executionDetail.summary.stage === 'completed' ? 'bg-emerald-500' : 'bg-brand'}`}
                        style={`width: ${executionDetail.summary.progress_percent}%`}
                      ></div>
                    </div>
                    <p class="mt-2 text-xs leading-5 text-slate-600">{executionDetail.summary.message}</p>
                  </div>

                  <div class="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                    <div class="rounded-lg border border-line bg-white p-3.5">
                      <p class="text-[10px] font-medium uppercase tracking-[.1em] text-slate-500">Records extracted</p>
                      <p class="mt-1.5 text-xl font-semibold text-ink">{formatNumber(executionDetail.summary.records_extracted)}</p>
                    </div>
                    <div class="rounded-lg border border-line bg-white p-3.5">
                      <p class="text-[10px] font-medium uppercase tracking-[.1em] text-slate-500">Records loaded</p>
                      <p class="mt-1.5 text-xl font-semibold text-ink">{formatNumber(executionDetail.summary.records_loaded)}</p>
                    </div>
                    <div class="rounded-lg border border-line bg-white p-3.5">
                      <p class="text-[10px] font-medium uppercase tracking-[.1em] text-slate-500">Streams</p>
                      <p class="mt-1.5 text-xl font-semibold text-ink">{formatNumber(executionDetail.summary.streams?.length || 0)}</p>
                    </div>
                    <div class="rounded-lg border border-line bg-white p-3.5">
                      <p class="text-[10px] font-medium uppercase tracking-[.1em] text-slate-500">Duration</p>
                      <p class="mt-1.5 text-xl font-semibold text-ink">{executionDuration(executionDetail.run)}</p>
                    </div>
                  </div>

                  <div class="mt-4">
                    <p class="mb-2 text-[10px] font-semibold uppercase tracking-[.12em] text-slate-500">Progress</p>
                    <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-6">
                      {#each executionDetail.summary.steps as step}
                        <div class={`rounded-lg border px-3 py-2.5 ${stepStyle(step.status)}`}>
                          <div class="flex items-center gap-2">
                            <span class="grid h-4 w-4 place-items-center rounded-full border border-current text-[9px]">
                              {#if step.status === 'completed'}✓{:else if step.status === 'failed'}!{:else if step.status === 'active'}•{:else}·{/if}
                            </span>
                            <span class="text-[11px] font-semibold">{step.label}</span>
                          </div>
                          {#if step.at}<p class="mt-1 pl-6 text-[9px] opacity-70">{formatLogTime(step.at)}</p>{/if}
                        </div>
                      {/each}
                    </div>
                  </div>

                  <div class="mt-5">
                    <div class="mb-2 flex items-center justify-between">
                      <p class="text-[10px] font-semibold uppercase tracking-[.12em] text-slate-500">Data moved</p>
                      <span class="text-[10px] text-slate-400">Detected from Singer metrics</span>
                    </div>
                    {#if executionDetail.summary.streams?.length}
                      <div class="overflow-x-auto rounded-lg border border-line">
                        <table class="w-full min-w-[480px] text-left text-xs">
                          <thead class="bg-slate-50 text-[10px] uppercase tracking-[.1em] text-slate-500">
                            <tr><th class="px-4 py-2.5 font-semibold">Stream</th><th class="px-4 py-2.5 font-semibold">Extracted</th><th class="px-4 py-2.5 font-semibold">Loaded</th><th class="px-4 py-2.5 font-semibold">Status</th></tr>
                          </thead>
                          <tbody class="divide-y divide-line bg-white">
                            {#each executionDetail.summary.streams as stream}
                              <tr>
                                <td class="px-4 py-2.5 font-medium text-slate-700">{stream.name}</td>
                                <td class="px-4 py-2.5 tabular-nums text-slate-700">{formatNumber(stream.records_extracted ?? stream.records)}</td>
                                <td class="px-4 py-2.5 tabular-nums text-slate-700">{formatNumber(stream.records_loaded)}</td>
                                <td class="px-4 py-2.5"><span class="status-pill bg-slate-100 text-slate-600">{stream.status}</span></td>
                              </tr>
                            {/each}
                          </tbody>
                        </table>
                      </div>
                    {:else}
                      <div class="rounded-lg border border-dashed border-slate-300 px-4 py-6 text-center text-xs text-slate-500">
                        No record metrics yet. They appear after the extractor starts processing a stream.
                      </div>
                    {/if}
                  </div>
                </div>

                <details class="border-t border-line">
                  <summary class="flex cursor-pointer list-none items-center justify-between gap-3 px-5 py-3.5 text-xs font-semibold text-slate-700 transition hover:bg-slate-50">
                    <span class="inline-flex items-center gap-2"><Icon name="server" size={15} /> Execution log</span>
                    <span class="text-[10px] font-normal text-slate-500">{executionDetail.log?.line_count || 0} lines · click to expand</span>
                  </summary>
                  <div class="max-h-96 overflow-auto border-t border-slate-800 bg-slate-950 px-4 py-3 font-mono text-[10px] leading-5 text-slate-300">
                    {#if executionDetail.log?.lines?.length}
                      {#each executionDetail.log.lines as line, index (index)}
                        <div class="grid grid-cols-[64px_92px_1fr] gap-2 border-b border-white/[.04] py-0.5">
                          <span class="text-slate-600">{formatLogTime(line.at)}</span>
                          <span class={`truncate ${line.level === 'error' ? 'text-rose-400' : line.level === 'warning' ? 'text-amber-400' : 'text-sky-400'}`}>{line.plugin || line.level || 'meltano'}</span>
                          <span class="break-words text-slate-300">{line.message}</span>
                        </div>
                      {/each}
                    {:else}
                      <p class="text-slate-500">No execution output yet. Log lines appear after the worker starts Meltano.</p>
                    {/if}
                  </div>
                </details>
              {:else}
                <div class="px-5 py-12 text-center text-sm text-slate-500">Select a run above to inspect its execution.</div>
              {/if}
            </section>
          {/if}
        {:else if page === 'Pipelines'}
          <section class="mb-6 flex flex-wrap items-end justify-between gap-4">
            <div>
              <p class="eyebrow text-brand">Data movement</p>
              <div class="mt-1.5 flex items-center gap-2"><h2 class="display text-2xl font-semibold text-ink">Integration pipelines</h2><span class="count-badge">{enabledPipelines} active</span></div>
              <p class="mt-2 text-sm text-slate-500">Manage source-to-destination data flows from one place.</p>
            </div>
            <button class="button-primary" on:click={openNewPipeline}><Icon name="plus" size={15} /> New pipeline</button>
          </section>

          <div class="toolbar">
            <div class="search-field">
              <label for="pipeline-search" class="sr-only">Search pipelines</label>
              <span class="search-field-icon"><Icon name="search" size={16} /></span>
              <input id="pipeline-search" type="search" bind:value={pipelineSearch} placeholder="Search pipeline, connector, or connection..." />
              {#if pipelineSearch}
                <button type="button" class="search-field-clear" aria-label="Clear pipeline search" on:click={() => pipelineSearch = ''}><Icon name="x" size={14} /></button>
              {/if}
            </div>
            <label for="pipeline-filter" class="sr-only">Filter pipelines by state</label>
            <select id="pipeline-filter" class="filter-select" bind:value={pipelineFilter}>
              <option value="all">All states</option>
              <option value="enabled">Enabled</option>
              <option value="disabled">Disabled</option>
              <option value="attention">Needs attention</option>
              <option value="no_runs">No runs</option>
            </select>
            {#if pipelineFiltersActive}
              <button type="button" class="text-xs font-semibold text-brand hover:text-brand-deep" on:click={() => { pipelineSearch = ''; pipelineFilter = 'all'; }}>Clear</button>
            {/if}
            <span class="ml-auto whitespace-nowrap text-xs text-slate-500" aria-live="polite"><strong class="font-semibold text-slate-700">{filteredPipelines.length}</strong> of {pipelines.length}</span>
          </div>

          {#if filteredPipelines.length}
            <div class="space-y-3">
              {#each filteredPipelines as pipeline (pipeline.id)}
                <article class="panel overflow-hidden">
                  <div class="flex flex-wrap items-center gap-5 p-5">
                    <div class="flex min-w-[220px] flex-1 items-center gap-4">
                      <span class={`grid h-9 w-9 place-items-center rounded-lg ${pipeline.enabled ? 'bg-blue-50 text-brand' : 'bg-slate-100 text-slate-400'}`}><Icon name="pipelines" size={18} /></span>
                      <div class="min-w-0">
                        <div class="flex flex-wrap items-center gap-2">
                          <p class="truncate text-sm font-semibold text-ink">{pipeline.name}</p>
                          <span class={`status-pill ${pipeline.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-600'}`}>{pipeline.enabled ? 'Enabled' : 'Disabled'}</span>
                          {#if latestRunByPipeline[pipeline.id]}
                            <span class={`status-pill ${statusStyle(latestRunByPipeline[pipeline.id].status)}`}>Last: {statusLabel(latestRunByPipeline[pipeline.id].status)}</span>
                          {/if}
                          {#if pipeline.source_filter?.streams?.length || pipeline.source_filter?.row_filter}
                            <span class="status-pill bg-violet-50 text-violet-700">
                              {pipeline.source_filter?.streams?.length ? `${pipeline.source_filter.streams.length} selected` : 'Row filter'}
                            </span>
                          {/if}
                        </div>
                        <p class="mt-1 text-[11px] text-slate-500">{pipeline.extractor} → {pipeline.loader}</p>
                      </div>
                    </div>

                    <div class="hidden min-w-[330px] items-center gap-3 rounded-lg border border-line bg-slate-50 px-3 py-2 md:flex">
                      <DatabaseIcon driver={connectionDriver(pipeline.source_connection_id)} size={22} />
                      <div class="min-w-0 flex-1"><p class="truncate text-xs font-semibold text-slate-700">{connectionName(pipeline.source_connection_id)}</p><p class="text-[10px] text-slate-500">Source</p></div>
                      <Icon name="arrow-right" size={15} />
                      <DatabaseIcon driver={connectionDriver(pipeline.destination_connection_id)} size={22} />
                      <div class="min-w-0 flex-1"><p class="truncate text-xs font-semibold text-slate-700">{connectionName(pipeline.destination_connection_id)}</p><p class="text-[10px] text-slate-500">Destination</p></div>
                    </div>

                    <div class="flex items-center gap-1.5">
                      <button class="button-secondary px-3 py-2 text-xs" disabled={!pipeline.enabled} on:click={() => runPipeline(pipeline)}><Icon name="play" size={14} /> Run</button>
                      <button class="icon-button" title="Edit pipeline" on:click={() => openEditPipeline(pipeline)}><Icon name="edit" size={16} /></button>
                      <button class="icon-button icon-button-danger" title="Delete pipeline" on:click={() => requestDelete('pipeline', pipeline)}><Icon name="trash" size={16} /></button>
                    </div>
                  </div>
                </article>
              {/each}
            </div>
          {:else if pipelines.length}
            <div class="empty-state">
              <span class="grid h-11 w-11 place-items-center rounded-lg bg-slate-100 text-slate-500"><Icon name="search" size={21} /></span>
              <h3 class="mt-4 text-sm font-semibold text-slate-800">No pipelines match</h3>
              <p class="mt-1 max-w-sm text-xs leading-5 text-slate-500">Try a different search term or reset the state filter.</p>
              <button class="button-secondary mt-5" on:click={() => { pipelineSearch = ''; pipelineFilter = 'all'; }}>Clear filters</button>
            </div>
          {:else}
            <div class="empty-state">
              <span class="grid h-11 w-11 place-items-center rounded-lg bg-blue-50 text-brand"><Icon name="pipelines" size={21} /></span>
              <h3 class="mt-4 text-sm font-semibold text-slate-800">No pipelines yet</h3>
              <p class="mt-1 max-w-sm text-xs leading-5 text-slate-500">Create a pipeline after adding at least one source and destination connection.</p>
              <button class="button-primary mt-5" on:click={openNewPipeline}><Icon name="plus" size={15} /> Create pipeline</button>
            </div>
          {/if}
        {:else if page === 'Connections'}
          <section class="mb-6 flex flex-wrap items-end justify-between gap-4">
            <div>
              <p class="eyebrow text-brand">Data sources and destinations</p>
              <h2 class="display mt-1.5 text-2xl font-semibold text-ink">Connections</h2>
              <p class="mt-2 text-sm text-slate-500">Manage database access. Credentials remain referenced and are resolved securely by the worker.</p>
            </div>
            <button class="button-primary" on:click={() => openNewConnection('source')}><Icon name="plus" size={15} /> Add connection</button>
          </section>
          <div class="toolbar">
            <div class="search-field">
              <label for="connection-search" class="sr-only">Search connections</label>
              <span class="search-field-icon"><Icon name="search" size={16} /></span>
              <input id="connection-search" type="search" bind:value={connectionSearch} placeholder="Search name, engine, host, or database..." />
              {#if connectionSearch}
                <button type="button" class="search-field-clear" aria-label="Clear connection search" on:click={() => connectionSearch = ''}><Icon name="x" size={14} /></button>
              {/if}
            </div>
            <label for="connection-filter" class="sr-only">Filter connections by engine</label>
            <select id="connection-filter" class="filter-select" bind:value={connectionFilter}>
              <option value="all">All engines</option>
              <option value="postgres">PostgreSQL</option>
              <option value="mysql">MySQL</option>
              <option value="mssql">SQL Server</option>
              <option value="oracle">Oracle</option>
              <option value="mongodb">MongoDB</option>
              <option value="rest">REST API</option>
              <option value="csv">CSV</option>
              <option value="google-sheets">Google Sheets</option>
              <option value="s3">Amazon S3</option>
              <option value="duckdb">DuckDB</option>
            </select>
            {#if connectionFiltersActive}
              <button type="button" class="text-xs font-semibold text-brand hover:text-brand-deep" on:click={() => { connectionSearch = ''; connectionFilter = 'all'; }}>Clear</button>
            {/if}
            <span class="ml-auto whitespace-nowrap text-xs text-slate-500" aria-live="polite"><strong class="font-semibold text-slate-700">{filteredSourceConnections.length + filteredDestinationConnections.length}</strong> of {connections.length}</span>
          </div>
          <div class="grid gap-6 xl:grid-cols-2">
            <ConnectionGroup title="Sources" items={filteredSourceConnections} kind="source" filtered={connectionFiltersActive} onEdit={openEditConnection} onDelete={(item) => requestDelete('connection', item)} />
            <ConnectionGroup title="Destinations" items={filteredDestinationConnections} kind="destination" filtered={connectionFiltersActive} onEdit={openEditConnection} onDelete={(item) => requestDelete('connection', item)} />
          </div>
        {:else if page === 'Users'}
          <section class="mb-6 flex flex-wrap items-end justify-between gap-4">
            <div>
              <p class="eyebrow text-brand">Dashboard access</p>
              <h2 class="display mt-1.5 text-2xl font-semibold text-ink">Users and roles</h2>
              <p class="mt-2 text-sm text-slate-500">Superadmin manages accounts. Admin can operate pipelines but cannot manage users.</p>
            </div>
            <button class="button-primary" on:click={openNewUser}><Icon name="plus" size={15} /> Add user</button>
          </section>
          <div class="panel overflow-hidden">
            <div class="grid grid-cols-[1fr_140px_100px] gap-4 border-b border-line bg-slate-50 px-5 py-3 text-[10px] font-bold uppercase tracking-[.14em] text-slate-500">
              <span>User</span><span>Role</span><span class="text-right">Actions</span>
            </div>
            {#if usersLoading}
              <div class="px-5 py-12 text-center text-sm text-slate-500">Loading users...</div>
            {:else}
              <div class="divide-y divide-line">
                {#each users as user (user.id)}
                  <div class="grid grid-cols-[1fr_140px_100px] items-center gap-4 px-5 py-4">
                    <div class="min-w-0">
                      <p class="truncate text-sm font-semibold text-slate-800">{user.display_name || user.username}</p>
                      <p class="mt-0.5 text-[11px] text-slate-500">@{user.username}</p>
                    </div>
                    <span class={`w-fit rounded-full px-2.5 py-1 text-[10px] font-bold capitalize ${user.role === 'superadmin' ? 'bg-blue-50 text-blue-700' : 'bg-slate-100 text-slate-600'}`}>{user.role}</span>
                    <div class="flex justify-end gap-1">
                      <button class="icon-button" title="Edit user" on:click={() => openEditUser(user)}><Icon name="edit" size={15} /></button>
                      {#if user.id !== currentUser?.id}
                        <button class="icon-button icon-button-danger" title="Delete user" on:click={() => requestDelete('user', user)}><Icon name="trash" size={15} /></button>
                      {/if}
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {:else}
          <section class="mb-6 flex flex-wrap items-end justify-between gap-4">
            <div>
              <p class="eyebrow text-brand">Integration capabilities</p>
              <h2 class="display mt-1.5 text-2xl font-semibold text-ink">Connector catalog</h2>
              <p class="mt-2 text-sm text-slate-500">Review extractors and loaders available to the selected workspace.</p>
            </div>
            <span class="count-badge">{connectorSource === 'tenant' ? 'Workspace plugins' : 'Global catalog'}</span>
          </section>
          <div class="toolbar">
            <div class="search-field">
              <label for="connector-search" class="sr-only">Search connectors</label>
              <span class="search-field-icon"><Icon name="search" size={16} /></span>
              <input id="connector-search" type="search" bind:value={connectorSearch} placeholder="Search connector name or provider..." />
              {#if connectorSearch}
                <button type="button" class="search-field-clear" aria-label="Clear connector search" on:click={() => connectorSearch = ''}><Icon name="x" size={14} /></button>
              {/if}
            </div>
            <label for="connector-filter" class="sr-only">Filter connectors by type</label>
            <select id="connector-filter" class="filter-select" bind:value={connectorFilter}>
              <option value="all">All types</option>
              <option value="extractor">Extractors</option>
              <option value="loader">Loaders</option>
            </select>
            {#if connectorFiltersActive}
              <button type="button" class="text-xs font-semibold text-brand hover:text-brand-deep" on:click={() => { connectorSearch = ''; connectorFilter = 'all'; }}>Clear</button>
            {/if}
            <span class="ml-auto whitespace-nowrap text-xs text-slate-500" aria-live="polite"><strong class="font-semibold text-slate-700">{filteredConnectors.length}</strong> of {connectors.length}</span>
          </div>
          {#if filteredConnectors.length}
          <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {#each filteredConnectors as connector (connector.name)}
              <article class="panel flex items-center gap-4 p-4">
                <DatabaseIcon driver={connector.driver} size={32} />
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-2">
                    <p class="truncate text-sm font-semibold text-slate-800">{connector.label || connector.name}</p>
                    {#if connector.installed === true}<span class="rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-semibold text-emerald-700">Installed</span>{/if}
                  </div>
                  <p class="mt-1 text-[11px] capitalize text-slate-500">{connector.kind} · {driverLabels[connector.driver] || connector.driver || 'Custom'}</p>
                </div>
              </article>
            {/each}
          </div>
          {:else if connectors.length}
            <div class="empty-state">
              <span class="grid h-11 w-11 place-items-center rounded-lg bg-slate-100 text-slate-500"><Icon name="search" size={21} /></span>
              <h3 class="mt-4 text-sm font-semibold text-slate-800">No connectors match</h3>
              <p class="mt-1 max-w-sm text-xs leading-5 text-slate-500">Try another connector name, provider, or type.</p>
              <button class="button-secondary mt-5" on:click={() => { connectorSearch = ''; connectorFilter = 'all'; }}>Clear filters</button>
            </div>
          {:else}
            <div class="empty-state">
              <span class="grid h-11 w-11 place-items-center rounded-lg bg-slate-100 text-slate-500"><Icon name="connectors" size={21} /></span>
              <h3 class="mt-4 text-sm font-semibold text-slate-800">No connectors available</h3>
              <p class="mt-1 max-w-sm text-xs leading-5 text-slate-500">Sync workspace plugins or check the control plane connector catalog.</p>
            </div>
          {/if}
        {/if}
      </div>
    </div>
  </main>
</div>

<nav class="fixed bottom-3 left-3 right-3 z-40 grid rounded-lg border border-white/10 bg-navy/95 p-1.5 shadow-2xl backdrop-blur-xl lg:hidden" style={`grid-template-columns: repeat(${visibleNav.length}, minmax(0, 1fr))`} aria-label="Mobile navigation">
  {#each visibleNav as item}
    <button class={`flex flex-col items-center gap-1 rounded-xl px-1 py-2 text-[10px] font-medium transition ${page === item.name ? 'bg-white/10 text-white' : 'text-slate-400'}`} on:click={() => goToPage(item.name)}>
      <span class={page === item.name ? 'text-brand' : ''}><Icon name={item.icon} size={18} /></span>
      {item.name}
    </button>
  {/each}
</nav>

{#if showTenantModal}
  <Modal title={editingTenant ? 'Edit workspace' : 'Create workspace'} eyebrow="Workspace settings" close={() => !busy && (showTenantModal = false)}>
    <p class="mb-5 text-sm leading-6 text-slate-500">Each workspace maps to an isolated Meltano tenant project.</p>
    <label class="field-label">
      Workspace key
      <input bind:value={tenantForm.name} disabled={!!editingTenant} placeholder="acme" />
      {#if editingTenant}<span class="field-hint">The project directory key cannot change while it is managed by CP_TENANTS_ROOT.</span>{/if}
    </label>
    <label class="field-label">Display name<input bind:value={tenantForm.display_name} placeholder="Acme production" /></label>
    <label class="field-label">Meltano environment<input bind:value={tenantForm.environment_name} placeholder="production" /></label>
    <div class="mt-2 flex justify-end gap-3">
      <button class="button-secondary" on:click={() => showTenantModal = false} disabled={busy}>Cancel</button>
      <button class="button-primary" on:click={saveTenant} disabled={busy || !tenantForm.name || !tenantForm.environment_name}>
        {busy ? 'Saving…' : editingTenant ? 'Save changes' : 'Create workspace'}
      </button>
    </div>
  </Modal>
{/if}

{#if showConnectionModal}
  <Modal title={editingConnection ? 'Edit connection' : 'Add connection'} eyebrow="Connector connection" size="lg" close={() => !connectionSaving && (showConnectionModal = false)}>
    <div class="mb-5 flex items-center gap-4 rounded-xl border border-line bg-slate-50 p-4">
      <DatabaseIcon driver={connectionForm.driver} size={38} />
      <div><p class="text-sm font-semibold text-slate-800">{driverLabels[connectionForm.driver]}</p><p class="mt-1 text-xs text-slate-500">{connectionForm.kind === 'source' ? 'Reads data from this connector' : 'Writes data to this connector'}</p></div>
    </div>

    <div class="grid gap-x-4 md:grid-cols-2">
      <label class="field-label">Connection name<input bind:value={connectionForm.name} disabled={connectionSaving} placeholder="orders-source" /></label>
      <label class="field-label">Role<select bind:value={connectionForm.kind} disabled={connectionSaving || !isDatabaseDriver(connectionForm.driver)}><option value="source">Source</option><option value="destination">Destination</option></select></label>
      <label class="field-label">Connection type<select bind:value={connectionForm.driver} on:change={updateDriverDefaults} disabled={connectionSaving}><option value="postgres">PostgreSQL</option><option value="mysql">MySQL</option><option value="mssql">SQL Server</option><option value="oracle">Oracle</option><option value="mongodb">MongoDB</option><option value="rest">REST API</option><option value="csv">CSV</option><option value="google-sheets">Google Sheets</option><option value="s3">Amazon S3</option><option value="duckdb">DuckDB</option></select></label>
      {#if isDatabaseDriver(connectionForm.driver)}
        <label class="field-label">Host<input bind:value={connectionForm.host} disabled={connectionSaving} placeholder="db.internal" /></label>
        <label class="field-label">Port<input bind:value={connectionForm.port} disabled={connectionSaving} placeholder={defaultPort(connectionForm.driver)} /></label>
        <label class="field-label">Database<input bind:value={connectionForm.database} disabled={connectionSaving} placeholder="orders" /></label>
        {#if ['postgres', 'mysql', 'mssql', 'mongodb'].includes(connectionForm.driver)}
          <label class="field-label">SSL mode<select bind:value={connectionForm.sslmode} disabled={connectionSaving}><option value="prefer">Prefer</option><option value="require">Require</option><option value="verify-full">Verify full</option><option value="disable">Disable</option></select></label>
        {/if}
        {#if ['postgres', 'mssql'].includes(connectionForm.driver)}
          <label class="field-label">Schema<input bind:value={connectionForm.schema} disabled={connectionSaving} placeholder="public" /></label>
        {/if}
        {#if connectionForm.driver === 'oracle'}
          <label class="field-label">Oracle service name<input bind:value={connectionForm.service_name} disabled={connectionSaving} placeholder="FINANCE" /></label>
          <label class="field-label">Oracle SID<input bind:value={connectionForm.sid} disabled={connectionSaving} placeholder="ORCL" /><span class="field-hint">Use service name or SID, not both.</span></label>
          <label class="field-label">Schema<input bind:value={connectionForm.schema} disabled={connectionSaving} placeholder="REPORTING" /></label>
        {/if}
        {#if connectionForm.driver === 'mongodb'}
          <label class="field-label">Auth source<input bind:value={connectionForm.auth_source} disabled={connectionSaving} placeholder="admin" /><span class="field-hint">Database used by MongoDB for authentication.</span></label>
        {/if}
        <label class="field-label">Username<input bind:value={connectionForm.username} disabled={connectionSaving} placeholder="integration_ro" /></label>
        {#if connectionForm.kind === 'destination' && ['postgres', 'mssql'].includes(connectionForm.driver)}
          <label class="field-label">Load strategy<select bind:value={connectionForm.load_method} disabled={connectionSaving}><option value="upsert">Upsert</option><option value="overwrite">Overwrite</option><option value="append-only">Append only</option></select></label>
        {/if}
      {:else if connectionForm.driver === 'rest'}
        <label class="field-label md:col-span-2">API base URL<input bind:value={connectionForm.api_url} disabled={connectionSaving} placeholder="https://api.example.com" /></label>
        <label class="field-label">Path<input bind:value={connectionForm.api_path} disabled={connectionSaving} placeholder="/v1/records" /></label>
        <label class="field-label">Stream name<input bind:value={connectionForm.stream_name} disabled={connectionSaving} placeholder="records" /><span class="field-hint">Required by tap-rest-api-msdk for stream discovery.</span></label>
        <label class="field-label">Primary keys<input bind:value={connectionForm.rest_primary_keys} disabled={connectionSaving} placeholder="id" /></label>
        <label class="field-label">Records path<input bind:value={connectionForm.rest_records_path} disabled={connectionSaving} placeholder="$[*]" /><span class="field-hint">JSONPath to the records array in the API response.</span></label>
        <label class="field-label">Authentication<select bind:value={connectionForm.auth_method} disabled={connectionSaving}><option value="no_auth">No auth</option><option value="basic">Basic</option><option value="api_key">API key</option><option value="bearer_token">Bearer token</option></select></label>
        {#if connectionForm.auth_method === 'basic'}
          <label class="field-label">Username<input bind:value={connectionForm.username} disabled={connectionSaving} /></label>
        {/if}
        {#if connectionForm.auth_method === 'api_key'}
          <label class="field-label">API key header<input bind:value={connectionForm.api_key_header} disabled={connectionSaving} placeholder="X-API-KEY" /></label>
        {/if}
      {:else if connectionForm.driver === 'csv'}
        <label class="field-label md:col-span-2">CSV file or directory<input bind:value={connectionForm.csv_path} disabled={connectionSaving} placeholder="data/input.csv" /><span class="field-hint">Path is relative to the Meltano project unless absolute.</span></label>
        <label class="field-label">Entity name<input bind:value={connectionForm.csv_entity} disabled={connectionSaving} placeholder="orders" /></label>
        <label class="field-label">Key columns<input bind:value={connectionForm.csv_keys} disabled={connectionSaving} placeholder="id, tenant_id" /></label>
        <label class="field-label">Encoding<input bind:value={connectionForm.csv_encoding} disabled={connectionSaving} placeholder="utf-8" /></label>
      {:else if connectionForm.driver === 'google-sheets'}
        <label class="field-label">Spreadsheet ID<input bind:value={connectionForm.sheet_id} disabled={connectionSaving} placeholder="1AbC..." /></label>
        <label class="field-label">Worksheet<input bind:value={connectionForm.child_sheet_name} disabled={connectionSaving} placeholder="Sheet1" /></label>
        <label class="field-label">Range<input bind:value={connectionForm.sheet_range} disabled={connectionSaving} placeholder="A1:Z1000" /></label>
      {:else if connectionForm.driver === 's3'}
        <label class="field-label">Bucket<input bind:value={connectionForm.s3_bucket} disabled={connectionSaving} placeholder="analytics-exports" /></label>
        <label class="field-label">Region<input bind:value={connectionForm.s3_region} disabled={connectionSaving} placeholder="ap-southeast-1" /></label>
        <label class="field-label">Prefix<input bind:value={connectionForm.s3_prefix} disabled={connectionSaving} placeholder="warehouse/" /></label>
        <label class="field-label">File format<select bind:value={connectionForm.s3_format} disabled={connectionSaving}><option value="parquet">Parquet</option><option value="csv">CSV</option><option value="jsonl">JSON Lines</option></select></label>
        <label class="field-label">Access key ID<input bind:value={connectionForm.username} disabled={connectionSaving} placeholder="Optional when using IAM role/profile" /></label>
        <label class="field-label">Custom endpoint<input bind:value={connectionForm.s3_endpoint} disabled={connectionSaving} placeholder="Optional S3-compatible endpoint" /></label>
      {:else if connectionForm.driver === 'duckdb'}
        <label class="field-label md:col-span-2">DuckDB file path<input bind:value={connectionForm.duckdb_path} disabled={connectionSaving} placeholder="output/warehouse.duckdb" /></label>
        <label class="field-label">Default schema<input bind:value={connectionForm.duckdb_schema} disabled={connectionSaving} placeholder="main" /></label>
      {/if}
    </div>

    {#if isDatabaseDriver(connectionForm.driver)}
      <div class="grid gap-x-4 md:grid-cols-2">
        <label class="field-label">Secret reference<input bind:value={connectionForm.secret_ref} disabled={connectionSaving} placeholder="vault://kv/data/team/db#password" /><span class="field-hint">Recommended. The platform stores only this reference.</span></label>
        <label class="field-label">Password<input type="password" bind:value={connectionForm.password} disabled={connectionSaving} placeholder={editingConnection ? 'Leave blank to keep current secret' : 'Local/demo only'} /><span class="field-hint">When supplied, database authentication is tested before save.</span></label>
      </div>
    {:else if ['rest', 'google-sheets', 's3'].includes(connectionForm.driver)}
      <div class="grid gap-x-4 md:grid-cols-2">
        <label class="field-label md:col-span-2">
          {connectionForm.driver === 'google-sheets' ? 'OAuth credentials secret reference' : connectionForm.driver === 's3' ? 'Secret access key reference' : 'Authentication secret reference'}
          <input bind:value={connectionForm.secret_ref} disabled={connectionSaving} placeholder={connectionForm.driver === 'google-sheets' ? 'vault://kv/data/google/oauth' : 'vault://kv/data/connector/secret'} />
          <span class="field-hint">
            {connectionForm.driver === 'google-sheets' ? 'Secret must contain JSON with client_id, client_secret, and refresh_token.' : connectionForm.driver === 'rest' && connectionForm.auth_method === 'no_auth' ? 'Optional when the API has no authentication.' : 'The credential value is resolved by the worker, not stored in the control plane.'}
          </span>
        </label>
      </div>
    {/if}

    {#if connectionStatus}<div class="mb-4 rounded-xl border border-sky-100 bg-sky-50 px-3.5 py-2.5 text-xs text-sky-800">{connectionStatus}</div>{/if}
    <div class="flex justify-end gap-3">
      <button class="button-secondary" on:click={() => showConnectionModal = false} disabled={connectionSaving}>Cancel</button>
      <button class="button-primary" on:click={saveConnection} disabled={connectionSaving || !connectionReady}>
        {connectionSaving ? 'Saving…' : editingConnection ? 'Save changes' : 'Add connection'}
      </button>
    </div>
  </Modal>
{/if}

{#if showPipelineModal}
  <Modal title={editingPipeline ? 'Edit pipeline' : 'Create pipeline'} eyebrow="Integration definition" size="lg" close={() => !busy && (showPipelineModal = false)}>
    <p class="mb-5 text-sm leading-6 text-slate-500">Choose the source and destination first. Compatible extractor and loader options update automatically.</p>
    <div class="grid gap-x-4 md:grid-cols-2" on:change={handlePipelineFormChange}>
      <label class="field-label md:col-span-2">Pipeline name<input bind:value={pipelineForm.name} placeholder="orders-to-backup" /></label>
      <label class="field-label">
        Extractor
        <select bind:value={pipelineForm.extractor} disabled={!extractorOptions.length || busy}>
          {#if !extractorOptions.length}<option value="">No compatible extractor installed</option>{/if}
          {#each extractorOptions as connector}<option value={connector.name}>{connector.label || connector.name} ({connector.name})</option>{/each}
        </select>
        <span class="field-hint">
          {pipelineSourceDriver ? `Filtered for ${driverLabels[pipelineSourceDriver] || pipelineSourceDriver} source.` : 'Select a source to see compatible extractors.'}
        </span>
      </label>
      <label class="field-label">
        Loader
        <select bind:value={pipelineForm.loader} disabled={!loaderOptions.length || busy}>
          {#if !loaderOptions.length}<option value="">No compatible loader installed</option>{/if}
          {#each loaderOptions as connector}<option value={connector.name}>{connector.label || connector.name} ({connector.name})</option>{/each}
        </select>
        <span class="field-hint">
          {pipelineDestinationDriver ? `Filtered for ${driverLabels[pipelineDestinationDriver] || pipelineDestinationDriver} destination.` : 'Select a destination to see compatible loaders.'}
        </span>
      </label>
      <label class="field-label">Source connection<select bind:value={pipelineForm.source_connection_id}><option value="">Select source</option>{#each sourceConnections as connection}<option value={connection.id}>{connection.name} · {driverLabels[connection.driver]}</option>{/each}</select></label>
      <label class="field-label">Destination connection<select bind:value={pipelineForm.destination_connection_id}><option value="">Select destination</option>{#each destinationConnections as connection}<option value={connection.id}>{connection.name} · {driverLabels[connection.driver]}</option>{/each}</select></label>
      <div class="mb-4 rounded-lg border border-line bg-slate-50/70 p-4 md:col-span-2">
        <div class="mb-3 flex items-center justify-between gap-3">
          <div>
            <p class="text-xs font-semibold text-slate-800">Source selection</p>
            <p class="mt-1 text-[11px] leading-5 text-slate-500">Leave tables empty to move every available stream.</p>
          </div>
          <span class="rounded-full bg-white px-2 py-1 text-[10px] font-semibold text-slate-500">Optional</span>
        </div>
        <label class="field-label">
          Tables or streams
          <textarea rows="3" bind:value={pipelineForm.source_streams} placeholder="public-orders, public-customers, appdb-employees"></textarea>
          <span class="field-hint">One stream per line or comma-separated. Exact names and wildcard patterns such as <code>sales-*</code> are supported.</span>
        </label>
        {#if pipelineForm.extractor === 'tap-oracle'}
          <div class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-[11px] leading-5 text-amber-800">
            Oracle supports table selection, but this legacy plugin does not support row filters.
          </div>
        {:else}
          <label class="field-label">
            Row filter
            <input bind:value={pipelineForm.row_filter} placeholder={'status == "active" and amount >= 100'} />
            <span class="field-hint">Singer expression applied while records leave the extractor. Use <code>==</code>, <code>!=</code>, comparisons, boolean operators, and quoted values.</span>
          </label>
        {/if}
      </div>
      {#if pipelineForm.loader === 'target-postgres'}
        <div class="mb-4 rounded-lg border border-blue-100 bg-blue-50/50 p-4 md:col-span-2">
          <div class="mb-3">
            <p class="text-xs font-semibold text-slate-800">Performance</p>
            <p class="mt-1 text-[11px] leading-5 text-slate-500">Tune the Python target for this pipeline without changing other pipelines.</p>
          </div>
          <div class="grid gap-x-4 md:grid-cols-2">
            <label class="field-label">
              Batch size rows
              <input type="number" min="1000" max="1000000" step="1000" bind:value={pipelineForm.transfer_batch_size} />
              <span class="field-hint">Larger batches reduce commit overhead but use more memory.</span>
            </label>
            <label class="field-label">
              Load method override
              <select bind:value={pipelineForm.transfer_load_method}>
                <option value="">Use connection default</option>
                <option value="upsert">Upsert</option>
                <option value="overwrite">Overwrite</option>
                <option value="append-only">Append only</option>
              </select>
              <span class="field-hint">Overwrite/append-only can speed up an initial full copy; use carefully.</span>
            </label>
            <label class="mb-1 flex items-center justify-between rounded-lg border border-line bg-white px-3.5 py-3 text-xs font-semibold text-slate-700 md:col-span-2">
              <span><span class="block">Validate record schema</span><span class="mt-0.5 block font-normal text-slate-500">Disable only for trusted already-validated source data.</span></span>
              <input type="checkbox" bind:checked={pipelineForm.transfer_validate_records} class="h-5 w-5 accent-blue-600" />
            </label>
          </div>
        </div>
      {/if}
      <label class="field-label">Timeout (seconds)<input type="number" min="1" bind:value={pipelineForm.default_timeout_seconds} /></label>
      <label class="field-label">Max attempts<input type="number" min="1" bind:value={pipelineForm.max_attempts} /></label>
      <label class="field-label md:col-span-2">State ID suffix<input bind:value={pipelineForm.state_id_suffix} placeholder="Optional, e.g. backfill-2026" /></label>
    </div>
    {#if pipelineSourceDriver && !extractorOptions.length}
      <div class="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-3.5 py-2.5 text-xs leading-5 text-amber-800">
        No compatible extractor is installed for this {driverLabels[pipelineSourceDriver] || pipelineSourceDriver} source. Provision {pipelineSourceDriver === 'mysql' ? 'tap-mysql' : 'a compatible extractor'} for this workspace, then refresh the connector catalog.
      </div>
    {/if}
    {#if pipelineDestinationDriver && !loaderOptions.length}
      <div class="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-3.5 py-2.5 text-xs leading-5 text-amber-800">
        No compatible loader is installed for this {driverLabels[pipelineDestinationDriver] || pipelineDestinationDriver} destination. Provision {pipelineDestinationDriver === 'postgres' ? 'target-postgres' : 'a compatible loader'} for this workspace, then refresh the connector catalog.
      </div>
    {/if}
    {#if pipelineStatus}<div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3.5 py-2.5 text-xs leading-5 text-rose-700">{pipelineStatus}</div>{/if}
    <label class="mb-5 flex items-center justify-between rounded-xl border border-line bg-slate-50 px-4 py-3">
      <span><span class="block text-sm font-semibold text-slate-800">Pipeline enabled</span><span class="mt-0.5 block text-xs text-slate-500">Disabled pipelines cannot start new runs.</span></span>
      <input type="checkbox" bind:checked={pipelineForm.enabled} class="h-5 w-5 accent-blue-600" />
    </label>
    <div class="flex justify-end gap-3">
      <button class="button-secondary" on:click={() => showPipelineModal = false} disabled={busy}>Cancel</button>
      <button class="button-primary" on:click={savePipeline} disabled={busy || !pipelineForm.name || !pipelineForm.source_connection_id || !pipelineForm.destination_connection_id || !pipelineForm.extractor || !pipelineForm.loader}>
        {busy ? 'Saving…' : editingPipeline ? 'Save changes' : 'Create pipeline'}
      </button>
    </div>
  </Modal>
{/if}

{#if showUserModal}
  <Modal title={editingUser ? 'Edit user' : 'Add user'} eyebrow="Dashboard access" close={() => !userSaving && (showUserModal = false)}>
    <label class="field-label">Username<input bind:value={userForm.username} disabled={userSaving} placeholder="operator" autocomplete="off" /></label>
    <label class="field-label">Display name<input bind:value={userForm.display_name} disabled={userSaving} placeholder="Operations Admin" /></label>
    <label class="field-label">
      Role
      <select bind:value={userForm.role} disabled={userSaving}>
        <option value="admin">Admin</option>
        <option value="superadmin">Superadmin</option>
      </select>
      <span class="field-hint">Superadmin can manage users. Admin can use the integration dashboard.</span>
    </label>
    <label class="field-label">
      {editingUser ? 'New password (optional)' : 'Password'}
      <input type="password" bind:value={userForm.password} disabled={userSaving} autocomplete="new-password" placeholder="Minimum 12 characters" />
    </label>
    {#if userStatus}<div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3.5 py-2.5 text-xs leading-5 text-rose-700">{userStatus}</div>{/if}
    <div class="flex justify-end gap-3">
      <button class="button-secondary" on:click={() => showUserModal = false} disabled={userSaving}>Cancel</button>
      <button class="button-primary" on:click={saveUser} disabled={userSaving || !userForm.username.trim() || (!editingUser && userForm.password.length < 12)}>
        {userSaving ? 'Saving...' : editingUser ? 'Save changes' : 'Create user'}
      </button>
    </div>
  </Modal>
{/if}

{#if showPasswordModal}
  <Modal title="Change password" eyebrow="Account security" close={() => !passwordSaving && (showPasswordModal = false)}>
    <label class="field-label">Current password<input type="password" bind:value={passwordForm.current_password} autocomplete="current-password" /></label>
    <label class="field-label">New password<input type="password" bind:value={passwordForm.new_password} autocomplete="new-password" /><span class="field-hint">Minimum 12 characters. Other active sessions will be revoked.</span></label>
    <label class="field-label">Confirm new password<input type="password" bind:value={passwordForm.confirm_password} autocomplete="new-password" /></label>
    {#if passwordStatus}<div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3.5 py-2.5 text-xs leading-5 text-rose-700">{passwordStatus}</div>{/if}
    <div class="flex justify-end gap-3">
      <button class="button-secondary" on:click={() => showPasswordModal = false} disabled={passwordSaving}>Cancel</button>
      <button class="button-primary" on:click={savePassword} disabled={passwordSaving || passwordForm.new_password.length < 12 || !passwordForm.current_password}>
        {passwordSaving ? 'Updating...' : 'Update password'}
      </button>
    </div>
  </Modal>
{/if}

{#if stopTarget}
  <ConfirmDialog
    title="Stop running pipeline?"
    message={`Stop “${pipelineName(stopTarget.pipeline_id)}”?`}
    detail="Meltano will be terminated gracefully. Batches already committed to the destination will remain, while the current uncommitted batch should roll back."
    confirmLabel="Stop run"
    busy={stopBusy}
    close={() => !stopBusy && (stopTarget = null)}
    confirm={confirmStopRun}
  />
{/if}

{#if deleteTarget}
  <ConfirmDialog
    title={deleteTarget.title}
    message={deleteTarget.message}
    detail={deleteTarget.detail}
    busy={deleteBusy}
    close={() => !deleteBusy && (deleteTarget = null)}
    confirm={confirmDelete}
  />
{/if}

{#if toast}
  <div class="fixed bottom-6 right-6 z-[60] flex items-center gap-3 rounded-xl border border-white/10 bg-navy px-4 py-3 text-sm font-medium text-white shadow-xl">
    <span class="grid h-6 w-6 place-items-center rounded-full bg-emerald-400/15 text-emerald-300"><Icon name="check" size={14} /></span>
    {toast}
  </div>
{/if}
{/if}
