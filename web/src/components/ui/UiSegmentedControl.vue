<script setup lang="ts">
import { useId } from 'vue'
defineProps<{label:string;options:{value:string;label:string}[];disabled?:boolean}>()
const model=defineModel<string>({required:true}); const name=useId()
</script>
<template><fieldset class="m-0 min-w-0 border-0 p-0" :disabled="disabled"><legend class="mb-2 font-semibold">{{ label }}</legend><div class="flex gap-1 rounded-control bg-neutral-soft p-1"><label v-for="option in options" :key="option.value" class="relative flex-1"><input v-model="model" type="radio" :name="name" :value="option.value" class="peer absolute opacity-0"><span class="flex min-h-11 items-center justify-center rounded-lg font-semibold text-muted peer-checked:bg-surface peer-checked:text-text peer-checked:shadow-card peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-accent">{{ option.label }}</span></label></div></fieldset></template>
