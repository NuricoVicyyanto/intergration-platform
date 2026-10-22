<script>
  export let name = 'activity';
  export let size = 18;
  export let strokeWidth = 1.8;
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {#if name === 'overview'}
    <rect x="3" y="3" width="7" height="7" rx="1.5" />
    <rect x="14" y="3" width="7" height="7" rx="1.5" />
    <rect x="3" y="14" width="7" height="7" rx="1.5" />
    <rect x="14" y="14" width="7" height="7" rx="1.5" />
  {:else if name === 'pipelines'}
    <circle cx="6" cy="6" r="2.5" />
    <circle cx="18" cy="18" r="2.5" />
    <path d="M8.5 6H14a4 4 0 0 1 4 4v5.5M6 8.5V15a3 3 0 0 0 3 3h6.5" />
  {:else if name === 'connections' || name === 'database'}
    <ellipse cx="12" cy="5" rx="7" ry="3" />
    <path d="M5 5v6c0 1.66 3.13 3 7 3s7-1.34 7-3V5" />
    <path d="M5 11v6c0 1.66 3.13 3 7 3s7-1.34 7-3v-6" />
  {:else if name === 'connectors'}
    <path d="M8 3v4M16 3v4M6 7h12v4a6 6 0 0 1-12 0V7Z" />
    <path d="M12 17v4" />
  {:else if name === 'edit'}
    <path d="M12 20h9" />
    <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L8 18l-4 1 1-4Z" />
  {:else if name === 'trash'}
    <path d="M3 6h18M8 6V4h8v2M19 6l-1 15H6L5 6" />
    <path d="M10 11v6M14 11v6" />
  {:else if name === 'plus'}
    <path d="M12 5v14M5 12h14" />
  {:else if name === 'play'}
    <path d="m8 5 11 7-11 7V5Z" />
  {:else if name === 'stop'}
    <rect x="6" y="6" width="12" height="12" rx="1.5" />
  {:else if name === 'x'}
    <path d="m6 6 12 12M18 6 6 18" />
  {:else if name === 'check'}
    <path d="m5 12 4 4L19 6" />
  {:else if name === 'chevron'}
    <path d="m8 10 4 4 4-4" />
  {:else if name === 'arrow-right'}
    <path d="M5 12h14M13 6l6 6-6 6" />
  {:else if name === 'server'}
    <rect x="3" y="4" width="18" height="6" rx="2" />
    <rect x="3" y="14" width="18" height="6" rx="2" />
    <path d="M7 7h.01M7 17h.01" />
  {:else if name === 'activity'}
    <path d="M3 12h4l2.5-7 4.5 14 2.5-7h4.5" />
  {:else if name === 'search'}
    <circle cx="11" cy="11" r="7" />
    <path d="m20 20-4-4" />
  {:else if name === 'api'}
    <path d="m8 8-4 4 4 4M16 8l4 4-4 4M14 5l-4 14" />
  {:else if name === 'table'}
    <rect x="3" y="4" width="18" height="16" rx="2" />
    <path d="M3 9h18M9 9v11" />
  {:else if name === 'workflow'}
    <rect x="3" y="3" width="6" height="5" rx="1.2" />
    <rect x="15" y="16" width="6" height="5" rx="1.2" />
    <path d="M9 5.5h3a3 3 0 0 1 3 3v5a3 3 0 0 0 3 3h-3" />
    <path d="m9 12 3 3-3 3" />
  {:else if name === 'settings'}
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.83 2.83-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1 1.55V21h-4v-.08a1.7 1.7 0 0 0-1-1.55 1.7 1.7 0 0 0-1.88.34l-.06.06-2.83-2.83.06-.06A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.55-1H3v-4h.08a1.7 1.7 0 0 0 1.55-1 1.7 1.7 0 0 0-.34-1.88l-.06-.06 2.83-2.83.06.06A1.7 1.7 0 0 0 9 4.6a1.7 1.7 0 0 0 1-1.55V3h4v.08a1.7 1.7 0 0 0 1 1.55 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.83 2.83-.06.06A1.7 1.7 0 0 0 19.4 9c.2.6.76 1 1.55 1H21v4h-.08c-.76 0-1.34.4-1.52 1Z" />
  {:else if name === 'users'}
    <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
    <circle cx="9" cy="7" r="4" />
    <path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
  {:else if name === 'warning'}
    <path d="M10.3 3.8 2.4 18a2 2 0 0 0 1.75 3h15.7a2 2 0 0 0 1.75-3L13.7 3.8a2 2 0 0 0-3.4 0Z" />
    <path d="M12 9v4M12 17h.01" />
  {:else if name === 'refresh'}
    <path d="M20 7h-5V2M4 17h5v5" />
    <path d="M5.5 9A8 8 0 0 1 19 7l1 2M18.5 15A8 8 0 0 1 5 17l-1-2" />
  {:else}
    <circle cx="12" cy="12" r="9" />
  {/if}
</svg>
