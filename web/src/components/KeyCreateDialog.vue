<script setup lang="ts">
import { ref, watch } from 'vue'
import UiDialog from './ui/UiDialog.vue'
import UiButton from './ui/UiButton.vue'
import UiField from './ui/UiField.vue'
import UiInput from './ui/UiInput.vue'
import UiSegmentedControl from './ui/UiSegmentedControl.vue'
import UiNotice from './ui/UiNotice.vue'
const props=defineProps<{open:boolean;busy:boolean;error:string}>(); const emit=defineEmits<{close:[];submit:[value:{name:string;expires_in_days:number|null}]}>()
const name=ref(''), mode=ref('unlimited'), days=ref<string|number>(90), nameError=ref(''), daysError=ref('')
watch(()=>props.open,open=>{if(open){name.value='';mode.value='unlimited';days.value=90;nameError.value='';daysError.value=''}})
watch(mode,()=>{days.value=90;daysError.value=''})
function submit(){const n=name.value.trim();nameError.value=!n?'Введите название':Array.from(n).length>80?'Не более 80 символов':'';const value=Number(days.value);daysError.value=mode.value==='days'&&(!Number.isSafeInteger(value)||value<1)?'Введите положительное целое число':'';if(!nameError.value&&!daysError.value)emit('submit',{name:n,expires_in_days:mode.value==='days'?value:null})}
</script>
<template><UiDialog :open="open" title="Создать ключ" description="Выдайте агенту отдельный ключ для доступа к MCP." :busy="busy" @close="emit('close')"><UiNotice v-if="error" tone="error">{{ error }}</UiNotice><form id="create-key-form" class="grid gap-4" @submit.prevent="submit"><UiField v-slot="field" id="key-name" label="Название" :error="nameError"><UiInput :id="field.id" v-model="name" :invalid="field.invalid" :aria-describedby="field.describedBy" maxlength="80" autocomplete="off" autofocus /></UiField><UiSegmentedControl v-model="mode" label="Срок действия" :options="[{value:'unlimited',label:'Бессрочно'},{value:'days',label:'На N дней'}]"/><UiField v-if="mode==='days'" v-slot="field" id="key-days" label="Количество дней" :error="daysError"><UiInput :id="field.id" v-model="days" type="number" min="1" step="1" :invalid="field.invalid" :aria-describedby="field.describedBy" /></UiField></form><template #footer><UiButton :disabled="busy" @click="emit('close')">Отмена</UiButton><UiButton type="submit" form="create-key-form" variant="primary" :loading="busy">Создать ключ</UiButton></template></UiDialog></template>
