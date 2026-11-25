<script setup lang="ts">withDefaults(defineProps<{shape?:'line'|'block'}>(),{shape:'line'})</script>
<template><div :class="shape === 'block' ? 'h-20' : 'h-3.5'" class="animate-pulse rounded-lg bg-neutral-soft motion-reduce:animate-none" aria-hidden="true"></div></template>
