<script>
  import postgresql from 'devicon/icons/postgresql/postgresql-original.svg';
  import mysql from 'devicon/icons/mysql/mysql-original.svg';
  import mssql from 'devicon/icons/microsoftsqlserver/microsoftsqlserver-original.svg';
  import oracle from 'devicon/icons/oracle/oracle-original.svg';
  import mongodb from 'devicon/icons/mongodb/mongodb-original.svg';
  import google from 'devicon/icons/google/google-original.svg';
  import aws from 'devicon/icons/amazonwebservices/amazonwebservices-original-wordmark.svg';
  import duckdb from 'devicon/icons/duckdb/duckdb-original.svg';
  import Icon from './Icon.svelte';

  export let driver = 'postgres';
  export let size = 34;

  $: source = {
    postgres: postgresql,
    mysql,
    mssql,
    oracle,
    mongodb,
    'google-sheets': google,
    s3: aws,
    duckdb
  }[String(driver || '').toLowerCase()];
  $: fallbackIcon = {
    rest: 'api',
    csv: 'table'
  }[String(driver || '').toLowerCase()];

  const labels = {
    postgres: 'PostgreSQL',
    mysql: 'MySQL',
    mssql: 'Microsoft SQL Server',
    oracle: 'Oracle',
    mongodb: 'MongoDB',
    rest: 'REST API',
    csv: 'CSV',
    'google-sheets': 'Google Sheets',
    s3: 'Amazon S3',
    duckdb: 'DuckDB'
  };
</script>

<span class="db-logo" style={`--logo-size:${size}px`}>
  {#if source}
    <img src={source} alt={`${labels[driver] || driver} logo`} width={size} height={size} />
  {:else if fallbackIcon}
    <span class={`db-logo-fallback ${driver === 'rest' ? 'text-sky-600' : 'text-emerald-600'}`}><Icon name={fallbackIcon} size={Math.round(size * 0.55)} /></span>
  {:else}
    <span class="db-logo-fallback"><Icon name="database" size={Math.round(size * 0.55)} /></span>
  {/if}
</span>
