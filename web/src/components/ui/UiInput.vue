<script setup lang="ts">
defineProps<{id:string;type?:'text'|'password'|'number';invalid?:boolean}>()
const model=defineModel<string|number>()
</script>
<template><input :id="id" v-model="model" :type="type || 'text'" :aria-invalid="invalid || undefined" class="min-h-11 w-full rounded-control border border-border bg-surface px-3 py-[9px] text-base leading-6 text-text transition-colors duration-120 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent aria-invalid:border-danger disabled:bg-neutral-soft read-only:font-mono motion-reduce:transition-none"></template>
