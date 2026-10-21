<script>
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';

  export let title = 'Delete item?';
  export let message = 'This action cannot be undone.';
  export let detail = '';
  export let confirmLabel = 'Delete';
  export let busy = false;
  export let close;
  export let confirm;
</script>

<Modal {title} eyebrow="Destructive action" {close}>
  <div class="mb-5 flex gap-4 rounded-xl border border-rose-100 bg-rose-50/70 p-4">
    <span class="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-white text-rose-600 shadow-sm">
      <Icon name="warning" size={20} />
    </span>
    <div>
      <p class="text-sm font-semibold text-slate-900">{message}</p>
      {#if detail}<p class="mt-1 text-xs leading-5 text-slate-600">{detail}</p>{/if}
    </div>
  </div>
  <div class="flex justify-end gap-3">
    <button class="button-secondary" on:click={close} disabled={busy}>Cancel</button>
    <button class="button-danger" on:click={confirm} disabled={busy}>{busy ? 'Deleting…' : confirmLabel}</button>
  </div>
</Modal>
