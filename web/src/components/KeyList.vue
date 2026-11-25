<script setup lang="ts">
import type { AgentKey } from './types'
import UiButton from './ui/UiButton.vue'
import UiBadge from './ui/UiBadge.vue'
import UiInput from './ui/UiInput.vue'
defineProps<{keys:AgentKey[]; revealed:Record<string,string>; busy:Record<string,boolean>}>()
const emit=defineEmits<{show:[id:string];hide:[id:string];copy:[id:string];revoke:[key:AgentKey];delete:[key:AgentKey]}>()
const date=(value:number)=>new Date(value*1000).toLocaleDateString('ru-RU',{year:'numeric',month:'short',day:'numeric'})
const statuses={active:'Активен',expired:'Истёк',revoked:'Отозван'} as const
function toggle(id:string,visible:boolean){if(visible)emit('hide',id);else emit('show',id)}
const tones={active:'success',expired:'warning',revoked:'neutral'} as const
</script>
<template>
 <div class="rounded-card border border-border bg-surface shadow-card">
  <div class="hidden grid-cols-[minmax(180px,2fr)_100px_110px_130px_minmax(200px,2fr)] gap-4 border-b border-border px-5 py-3.5 text-xs leading-[18px] font-semibold text-muted lg:grid">
   <span>Название</span><span>Статус</span><span>Создан</span><span>Срок</span><span>Управление</span>
  </div>
  <div v-for="key in keys" :key="key.id" class="grid grid-cols-2 items-center gap-3 border-b border-border p-5 last:border-0 lg:grid-cols-[minmax(180px,2fr)_100px_110px_130px_minmax(200px,2fr)] lg:gap-4 lg:px-5 lg:py-4">
   <div class="col-span-full lg:col-span-1"><div class="font-semibold [overflow-wrap:anywhere]">{{ key.name }}</div><div class="mt-1 font-mono text-xs leading-[18px] text-muted [overflow-wrap:anywhere]">{{ key.id }}</div></div>
   <div><span class="mb-1 block text-xs text-muted lg:hidden">Статус</span><UiBadge :tone="tones[key.status]">{{ statuses[key.status] }}</UiBadge></div>
   <div class="text-muted"><span class="mb-1 block text-xs lg:hidden">Создан</span>{{ date(key.created_at) }}</div>
   <div class="text-muted"><span class="mb-1 block text-xs lg:hidden">Срок</span>{{ key.expires_at===null ? 'Бессрочно' : date(key.expires_at) }}</div>
   <div class="col-span-full flex flex-wrap gap-1 lg:col-span-1">
    <template v-if="key.can_reveal"><UiButton variant="ghost" :disabled="busy[key.id]" @click="toggle(key.id,!!revealed[key.id])">{{ revealed[key.id] ? 'Скрыть' : 'Показать' }}</UiButton><UiButton variant="ghost" :disabled="busy[key.id]" @click="emit('copy',key.id)">Скопировать</UiButton></template>
    <UiButton v-if="key.status!=='revoked'" variant="ghost" :disabled="busy[key.id]" @click="emit('revoke',key)">Отозвать</UiButton><UiButton variant="ghost" :disabled="busy[key.id]" @click="emit('delete',key)">Удалить</UiButton>
   </div>
   <p v-if="!key.can_reveal" class="col-span-full text-xs leading-[18px] text-muted">Значение недоступно. Этот ключ создан до поддержки повторного просмотра. Создайте новый ключ, если значение потеряно.</p>
   <div v-if="revealed[key.id]" class="col-span-full flex gap-2"><UiInput :id="`secret-${key.id}`" :model-value="revealed[key.id]" readonly :aria-label="`Значение ключа ${key.name}`" /></div>
  </div>
 </div>
</template>
