<script setup lang="ts">
import type { Site } from './types'
import UiBadge from './ui/UiBadge.vue'
import UiButton from './ui/UiButton.vue'
defineProps<{sites:Site[]}>()
const emit=defineEmits<{delete:[site:Site];domains:[site:Site]}>()
const states:Record<string,string>={pending:'Ожидает загрузки',receiving:'Загружается',ready:'Готов к публикации',failed:'Ошибка загрузки',expired:'Срок загрузки истёк'}
const errors:Record<string,string>={invalid_archive:'Некорректный архив',archive_too_large:'Архив слишком большой',upload_expired:'Срок загрузки истёк',upload_interrupted:'Загрузка прервана',site_not_found:'Сайт удалён'}
</script>
<template>
 <div class="rounded-card border border-border bg-surface shadow-card">
  <div v-for="site in sites" :key="site.id" class="grid gap-3 border-b border-border p-5 last:border-0 md:grid-cols-[minmax(0,1fr)_auto]">
   <div class="grid gap-2">
    <strong class="[overflow-wrap:anywhere]">{{ site.name }}</strong>
    <a v-if="site.status==='published'" :href="site.url" target="_blank" rel="noopener noreferrer" class="text-accent [overflow-wrap:anywhere]">{{ site.url }}</a>
    <span v-else class="text-muted [overflow-wrap:anywhere]">{{ site.url }}</span>
    <div><UiBadge :tone="site.status==='published'?'success':'neutral'">{{ site.status==='published'?'Опубликован':'Не опубликован' }}</UiBadge> <span class="text-muted">Поколение {{ site.generation }} · {{ site.size_bytes }} байт · файлов: {{ site.file_count }}</span></div>
    <p v-if="site.upload" class="text-sm text-muted">{{ states[site.upload.state] || site.upload.state }}<span v-if="site.upload.error_code">: {{ errors[site.upload.error_code] || 'Не удалось подготовить загрузку' }}</span></p>
   </div>
   <div class="flex flex-wrap items-start gap-2">
    <UiButton variant="ghost" :aria-label="`Домены сайта ${site.name}`" @click="emit('domains',site)">Домены</UiButton>
    <UiButton variant="ghost" :aria-label="`Удалить сайт ${site.name}`" @click="emit('delete',site)">Удалить</UiButton>
   </div>
  </div>
 </div>
</template>
