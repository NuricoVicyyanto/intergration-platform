<script>
  export let title;
  export let close;
  export let eyebrow = '';
  export let size = 'md';
</script>

<div
  class="modal-backdrop"
  role="presentation"
  on:click={(event) => event.target === event.currentTarget && close()}
>
  <section
    class:modal-wide={size === 'lg'}
    class="modal-shell"
    role="dialog"
    aria-modal="true"
    aria-label={title}
  >
    <div class="mb-6 flex items-start justify-between gap-5">
      <div>
        {#if eyebrow}<p class="eyebrow mb-2">{eyebrow}</p>{/if}
        <h2 class="display text-xl font-semibold tracking-tight text-ink">{title}</h2>
      </div>
      <button class="icon-button" on:click={close} aria-label="Close">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
          <path d="m6 6 12 12M18 6 6 18" />
        </svg>
      </button>
    </div>
    <slot />
  </section>
</div>
