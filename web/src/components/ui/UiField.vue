<script setup lang="ts">
import { computed } from 'vue'
const props=defineProps<{id:string;label:string;hint?:string;error?:string}>()
const describedBy=computed(()=>props.error || props.hint ? `${props.id}-description` : undefined)
</script>
<template><div class="grid gap-2"><label :for="id" class="font-semibold">{{ label }}</label><slot :id="id" :described-by="describedBy" :invalid="!!error"/><p v-if="error" :id="describedBy" class="text-xs leading-[18px] text-danger" role="alert">{{ error }}</p><p v-else-if="hint" :id="describedBy" class="text-xs leading-[18px] text-muted">{{ hint }}</p></div></template>
