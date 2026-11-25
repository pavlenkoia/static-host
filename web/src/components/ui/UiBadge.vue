<script setup lang="ts">
defineProps<{tone:'success'|'warning'|'neutral'}>()
const tones = {success:'bg-success-soft text-success',warning:'bg-warning-soft text-warning',neutral:'bg-neutral-soft text-muted'} as const
</script>
<template><span :class="tones[tone]" class="inline-flex items-center rounded-full px-2.5 py-[3px] text-xs leading-[18px] font-semibold whitespace-nowrap"><slot /></span></template>
