<script>
  import DatabaseIcon from './DatabaseIcon.svelte';
  import Icon from './Icon.svelte';

  export let title;
  export let items = [];
  export let kind;
  export let filtered = false;
  export let onEdit = () => {};
  export let onDelete = () => {};
</script>

<section class="panel overflow-hidden p-0">
  <div class="flex items-center justify-between border-b border-line px-5 py-4">
    <div class="flex items-center gap-3">
      <span class={`grid h-9 w-9 place-items-center rounded-lg ${kind === 'source' ? 'bg-sky-50 text-sky-700' : 'bg-emerald-50 text-emerald-700'}`}>
        <Icon name={kind === 'source' ? 'server' : 'database'} size={18} />
      </span>
      <div>
        <h3 class="text-sm font-semibold text-ink">{title}</h3>
        <p class="mt-0.5 text-xs text-slate-500">{kind === 'source' ? 'Systems that provide data' : 'Systems that receive data'}</p>
      </div>
    </div>
    <span class="count-badge">{items.length}</span>
  </div>

  {#if items.length}
    <div class="divide-y divide-line">
      {#each items as connection (connection.id)}
        <article class="group flex items-center gap-4 px-5 py-4 transition hover:bg-slate-50/80">
          <div class="connection-logo">
            <DatabaseIcon driver={connection.driver} size={36} />
          </div>
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2">
              <p class="truncate text-sm font-semibold text-ink">{connection.name}</p>
              <span class={`h-1.5 w-1.5 rounded-full ${kind === 'source' ? 'bg-sky-500' : 'bg-emerald-500'}`}></span>
            </div>
            <p class="mt-1 truncate text-xs text-slate-500">
              {connection.driver} · {connection.endpoint?.host || 'Host hidden'} · {connection.endpoint?.database || 'Database hidden'}
            </p>
          </div>
          <div class="flex items-center gap-1 opacity-80 transition group-hover:opacity-100">
            <button class="icon-button" title="Edit connection" aria-label={`Edit ${connection.name}`} on:click={() => onEdit(connection)}>
              <Icon name="edit" size={16} />
            </button>
            <button class="icon-button icon-button-danger" title="Delete connection" aria-label={`Delete ${connection.name}`} on:click={() => onDelete(connection)}>
              <Icon name="trash" size={16} />
            </button>
          </div>
        </article>
      {/each}
    </div>
  {:else}
    <div class="px-6 py-12 text-center">
      <span class="mx-auto grid h-10 w-10 place-items-center rounded-lg bg-slate-100 text-slate-400">
        <Icon name="database" size={22} />
      </span>
      <p class="mt-4 text-sm font-semibold text-slate-700">{filtered ? `No ${kind} connections found` : `No ${kind} connections`}</p>
      <p class="mt-1 text-xs text-slate-500">{filtered ? 'Adjust or clear the search and filter.' : 'Add a database to this side of the pipeline.'}</p>
    </div>
  {/if}
</section>
