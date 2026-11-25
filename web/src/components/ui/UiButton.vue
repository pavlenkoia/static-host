<script setup lang="ts">
withDefaults(defineProps<{ variant?: 'primary'|'secondary'|'ghost'|'danger'; type?: 'button'|'submit'; loading?: boolean; disabled?: boolean }>(), { variant:'secondary', type:'button' })
const variants = {
 primary: 'border-transparent bg-accent text-white hover:bg-accent-hover',
 secondary: 'border-border bg-surface text-text hover:bg-neutral-soft',
 ghost: 'border-transparent bg-transparent text-muted hover:bg-neutral-soft',
 danger: 'border-transparent bg-danger text-white hover:bg-danger-hover',
} as const
</script>
<template>
 <button :type="type" :class="variants[variant]" class="inline-flex min-h-11 cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded-control border px-4 font-semibold transition-colors duration-120 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-55 motion-reduce:transition-none" :disabled="disabled || loading" :aria-busy="loading || undefined">
  <span v-if="loading" class="size-[15px] animate-spin rounded-full border-2 border-current border-r-transparent motion-reduce:animate-none" aria-hidden="true"></span><slot />
 </button>
</template>
