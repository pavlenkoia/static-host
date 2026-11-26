<script setup lang="ts">
import { ref, watch, nextTick, useId, onBeforeUnmount } from 'vue'
import UiButton from './UiButton.vue'
const props=defineProps<{open:boolean;title:string;description?:string;busy?:boolean}>(); const emit=defineEmits<{close:[]}>()
const dialog=ref<HTMLDialogElement>(); const titleId=useId(), descriptionId=useId(); let previous:HTMLElement|null=null
watch(()=>props.open,async open=>{await nextTick(); const el=dialog.value; if(!el)return; if(open&&!el.open){previous=document.activeElement as HTMLElement; el.showModal()} else if(!open&&el.open){el.close();previous?.focus()}},{immediate:true})
onBeforeUnmount(()=>{if(dialog.value?.open){dialog.value.close();previous?.focus()}})
function cancel(e:Event){e.preventDefault();if(!props.busy)emit('close')}
</script>
<template>
  <dialog ref="dialog" class="m-auto max-h-[calc(100dvh-32px)] w-[calc(100%-32px)] max-w-[480px] overflow-hidden rounded-dialog border border-border bg-surface p-0 text-text shadow-dialog backdrop:bg-backdrop" :aria-labelledby="titleId" :aria-describedby="description ? descriptionId : undefined" @cancel="cancel">
    <div class="flex max-h-[calc(100dvh-34px)] flex-col">
      <header class="flex shrink-0 items-start justify-between gap-3 px-4 pt-6 md:px-6">
        <div>
          <h2 :id="titleId" class="text-xl leading-7 font-semibold">{{ title }}</h2>
          <p v-if="description" :id="descriptionId" class="mt-2 text-muted">{{ description }}</p>
        </div>
        <UiButton variant="ghost" :disabled="busy" aria-label="Закрыть окно" @click="emit('close')">×</UiButton>
      </header>
      <div class="grid min-h-0 gap-5 overflow-auto px-4 py-6 md:px-6"><slot /></div>
      <footer v-if="$slots.footer" class="flex shrink-0 justify-end gap-2 border-t border-border px-4 py-4 md:px-6"><slot name="footer" /></footer>
    </div>
  </dialog>
</template>
