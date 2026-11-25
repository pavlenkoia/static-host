<script setup lang="ts">
defineProps<{tone:'success'|'error'|'info';title?:string}>()
const tones = {success:'bg-success-soft text-success',error:'bg-danger-soft text-danger',info:'bg-accent-soft text-text'} as const
</script>
<template><div :class="tones[tone]" class="flex items-center justify-between gap-4 rounded-card border border-border p-4" :role="tone==='error' ? 'alert' : 'status'"><div><strong v-if="title">{{ title }}</strong><div><slot /></div></div><slot name="action" /></div></template>
