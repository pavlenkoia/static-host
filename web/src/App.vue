<script setup lang="ts">
import { onMounted,onBeforeUnmount,ref,watch } from 'vue'
import { useRouter } from 'vue-router'
import type { AgentKey, Site } from './components/types'
import KeyList from './components/KeyList.vue'
import SiteList from './components/SiteList.vue'
import KeyCreateDialog from './components/KeyCreateDialog.vue'
import UiButton from './components/ui/UiButton.vue'
import UiField from './components/ui/UiField.vue'
import UiInput from './components/ui/UiInput.vue'
import UiDialog from './components/ui/UiDialog.vue'
import UiNotice from './components/ui/UiNotice.vue'
import UiEmptyState from './components/ui/UiEmptyState.vue'
import UiSkeleton from './components/ui/UiSkeleton.vue'
const router=useRouter()
const user=ref(''),csrf=ref(''),login=ref(''),password=ref(''),sessionError=ref(''),loginError=ref(''),listError=ref(''),dialogError=ref(''),notice=ref('')
const sessionLoading=ref(true),listLoading=ref(false),loginBusy=ref(false),logoutBusy=ref(false),createBusy=ref(false),createOpen=ref(false),confirmBusy=ref(false)
const keys=ref<AgentKey[]>([]),revealed=ref<Record<string,string>>({}),busy=ref<Record<string,boolean>>({}),confirmation=ref<{key:AgentKey;action:'revoke'|'delete'}|null>(null)
const sites=ref<Site[]>([]),siteError=ref(''),siteLoading=ref(false),siteDialogError=ref(''),siteDeleting=ref(false),siteConfirmation=ref<Site|null>(null)
let generation=0,sessionGeneration=0
function clearSecrets(){generation++;revealed.value={}}
function clearSession(){sessionGeneration++;clearSecrets();user.value='';csrf.value='';keys.value=[];sites.value=[];siteConfirmation.value=null;siteError.value='';siteDialogError.value='';createOpen.value=false;confirmation.value=null}
function visibility(){if(document.hidden)clearSecrets()}
watch(()=>router.currentRoute.value.path,clearSecrets)
onMounted(()=>{document.addEventListener('visibilitychange',visibility);refresh()})
onBeforeUnmount(()=>{document.removeEventListener('visibilitychange',visibility);clearSecrets()})
async function api(path:string,method='GET',body?:unknown){
 const headers:Record<string,string>={};if(body!==undefined)headers['Content-Type']='application/json';if(method!=='GET'&&csrf.value)headers['X-CSRF-Token']=csrf.value
 const response=await fetch(path,{method,headers,credentials:'same-origin',...(body!==undefined?{body:JSON.stringify(body)}:{})})
 if(!response.ok){if(response.status===401&&path!=='/api/login'){clearSession();await router.replace('/login')}throw new Error(response.status===401&&path==='/api/login'?'Неверный логин или пароль':`Ошибка сервиса (${response.status})`)}
 return response.status===204?null:response.json()
}
const message=(e:unknown)=>e instanceof Error?e.message:'Ошибка сервиса'
async function loadKeys(){listLoading.value=true;listError.value='';try{keys.value=(await api('/api/keys')).keys}catch(e){if(user.value)listError.value=message(e)}finally{listLoading.value=false}}
async function loadSites(){const current=sessionGeneration;siteLoading.value=true;siteError.value='';try{const result=await api('/api/sites');if(current===sessionGeneration&&user.value)sites.value=result.sites}catch(e){if(current===sessionGeneration&&user.value)siteError.value=message(e)}finally{if(current===sessionGeneration)siteLoading.value=false}}
async function refresh(){sessionLoading.value=true;sessionError.value='';try{const s=await api('/api/session');user.value=s.login;csrf.value=s.csrf_token;await router.replace('/');await Promise.all([loadKeys(),loadSites()])}catch(e){if(!message(e).includes('(401)'))sessionError.value=message(e)}finally{sessionLoading.value=false}}
async function signIn(){loginBusy.value=true;loginError.value='';try{const s=await api('/api/login','POST',{login:login.value,password:password.value});user.value=s.login;csrf.value=s.csrf_token;await router.replace('/');await Promise.all([loadKeys(),loadSites()])}catch(e){loginError.value=message(e)}finally{password.value='';loginBusy.value=false}}
async function signOut(){logoutBusy.value=true;try{await api('/api/logout','POST');clearSession();await router.replace('/login')}catch(e){listError.value=message(e)}finally{logoutBusy.value=false}}
async function create(input:{name:string;expires_in_days:number|null}){createBusy.value=true;dialogError.value='';try{const result=await api('/api/keys','POST',input);keys.value=[result.key,...keys.value];createOpen.value=false;notice.value='Ключ создан'}catch(e){dialogError.value=message(e)}finally{createBusy.value=false}}
function openCreate(){dialogError.value='';createOpen.value=true}
async function reveal(id:string,show:boolean){const current=generation;busy.value[id]=true;try{const result=await api(`/api/keys/${encodeURIComponent(id)}/reveal`,'POST');if(current!==generation||!user.value||document.hidden)return;if(show)revealed.value[id]=result.token;else {try{await navigator.clipboard.writeText(result.token);if(current===generation)notice.value='Скопировано'}catch{if(current===generation)listError.value='Не удалось скопировать. Нажмите «Показать» и скопируйте значение вручную.'}}}catch(e){if(current===generation)listError.value=message(e)}finally{delete busy.value[id]}}
function hide(id:string){delete revealed.value[id];generation++}
async function copy(id:string){if(!revealed.value[id])return reveal(id,false);try{await navigator.clipboard.writeText(revealed.value[id]);notice.value='Скопировано'}catch{listError.value='Не удалось скопировать. Скопируйте показанное значение вручную.'}}
function confirmAction(key:AgentKey,action:'revoke'|'delete'){dialogError.value='';confirmation.value={key,action}}
async function perform(){const c=confirmation.value;if(!c)return;confirmBusy.value=true;busy.value[c.key.id]=true;dialogError.value='';try{const path=`/api/keys/${encodeURIComponent(c.key.id)}`;await api(c.action==='revoke'?path+'/revoke':path,c.action==='revoke'?'POST':'DELETE');if(c.action==='delete'){keys.value=keys.value.filter(k=>k.id!==c.key.id);hide(c.key.id)}else{const k=keys.value.find(k=>k.id===c.key.id);if(k){k.status='revoked';k.revoked_at=Math.floor(Date.now()/1000)}}confirmation.value=null;notice.value=c.action==='delete'?'Ключ удалён':'Ключ отозван'}catch(e){dialogError.value=message(e)}finally{delete busy.value[c.key.id];confirmBusy.value=false}}
function confirmSite(site:Site){siteDialogError.value='';siteConfirmation.value=site}
async function deleteSite(){const site=siteConfirmation.value;if(!site)return;siteDeleting.value=true;siteDialogError.value='';try{await api(`/api/sites/${encodeURIComponent(site.id)}`,'DELETE');sites.value=sites.value.filter(s=>s.id!==site.id);siteConfirmation.value=null;notice.value='Сайт удалён'}catch(e){siteDialogError.value=message(e)}finally{siteDeleting.value=false}}
</script>
<template>
 <div v-if="sessionLoading" class="grid min-h-dvh place-items-center p-6">
  <div class="grid w-full max-w-[420px] gap-4 rounded-card border border-border bg-surface p-6 shadow-card md:p-8" aria-busy="true"><span class="sr-only">Загрузка</span><UiSkeleton/><UiSkeleton shape="block"/></div>
 </div>
 <div v-else-if="sessionError" class="grid min-h-dvh place-items-center p-6"><UiNotice tone="error" title="Не удалось проверить сессию">{{ sessionError }}<template #action><UiButton @click="refresh">Повторить</UiButton></template></UiNotice></div>
 <main v-else-if="!user" class="grid min-h-dvh place-items-center p-6">
  <section class="w-full max-w-[420px] rounded-card border border-border bg-surface p-6 shadow-card md:p-8">
   <div class="mb-10 flex items-center gap-2.5 text-[17px] font-semibold tracking-tight"><span class="grid size-8 place-items-center rounded-control bg-accent text-white">s</span> static-host</div>
   <h1 class="text-[28px] leading-9 font-semibold tracking-tight">Вход в кабинет</h1><p class="mt-2 text-muted">Войдите, чтобы управлять доступом агентов.</p>
   <form class="mt-8 grid gap-5" @submit.prevent="signIn">
    <UiNotice v-if="loginError" tone="error">{{ loginError }}</UiNotice>
    <UiField v-slot="field" id="login" label="Логин"><UiInput :id="field.id" v-model="login" autocomplete="username" required /></UiField>
    <UiField v-slot="field" id="password" label="Пароль"><UiInput :id="field.id" v-model="password" type="password" autocomplete="current-password" required /></UiField>
    <UiButton class="w-full" variant="primary" type="submit" :loading="loginBusy">Войти</UiButton>
   </form>
  </section>
 </main>
 <div v-else class="mx-auto max-w-[1120px] px-4 md:px-6">
  <header class="flex h-[72px] items-center justify-between border-b border-border md:h-20">
   <div class="flex items-center gap-2.5 text-[17px] font-semibold tracking-tight"><span class="grid size-8 place-items-center rounded-control bg-accent text-white">s</span> static-host</div>
   <div class="flex items-center gap-3 text-muted"><span class="hidden md:inline">{{ user }}</span><UiButton variant="ghost" :loading="logoutBusy" @click="signOut">Выйти</UiButton></div>
  </header>
  <main class="pt-8 pb-20 md:pt-12">
   <section class="mb-12" aria-labelledby="sites-heading">
    <div class="mb-6 flex flex-wrap items-center justify-between gap-4"><h1 id="sites-heading" class="text-[28px] font-semibold tracking-tight">Сайты</h1><UiButton @click="loadSites">Обновить список</UiButton></div>
    <UiNotice v-if="siteError" tone="error">{{ siteError }}<template #action><UiButton @click="loadSites">Повторить</UiButton></template></UiNotice>
    <div v-if="siteLoading" aria-busy="true" class="grid gap-4 rounded-card border border-border bg-surface p-6"><span class="sr-only">Загрузка сайтов</span><UiSkeleton/><UiSkeleton shape="block"/></div>
    <template v-else-if="!siteError || sites.length"><SiteList v-if="sites.length" :sites="sites" @delete="confirmSite"/><div v-else class="rounded-card border border-border bg-surface shadow-card"><UiEmptyState title="Сайтов пока нет" description="Создайте сайт через MCP."/></div></template>
   </section>
   <div class="mb-8 grid gap-5 md:flex md:items-start md:justify-between md:gap-6">
    <div><h1 class="text-[28px] leading-9 font-semibold tracking-tight md:text-[32px] md:leading-10">Ключи агентов</h1><p class="mt-2 text-base leading-6 text-muted">Управляйте доступом агентов к вашему кабинету.</p></div>
    <UiButton class="w-full md:w-auto" variant="primary" @click="openCreate">Создать ключ</UiButton>
   </div>
   <div class="grid gap-4">
    <div aria-live="polite"><UiNotice v-if="notice" tone="success">{{ notice }}</UiNotice></div>
    <UiNotice v-if="listError" tone="error">{{ listError }}<template #action><UiButton @click="loadKeys">Повторить</UiButton></template></UiNotice>
    <div v-if="listLoading" class="grid gap-4 rounded-card border border-border bg-surface p-6 shadow-card" aria-busy="true"><span class="sr-only">Загрузка ключей</span><UiSkeleton/><UiSkeleton shape="block"/><UiSkeleton shape="block"/></div>
    <template v-else-if="!listError || keys.length">
     <KeyList v-if="keys.length" :keys="keys" :revealed="revealed" :busy="busy" @show="id=>reveal(id,true)" @hide="hide" @copy="copy" @revoke="key=>confirmAction(key,'revoke')" @delete="key=>confirmAction(key,'delete')"/>
     <div v-else class="rounded-card border border-border bg-surface shadow-card"><UiEmptyState title="Ключей пока нет" description="Создайте ключ, чтобы подключить агента к MCP."><template #action><UiButton variant="primary" @click="openCreate">Создать ключ</UiButton></template></UiEmptyState></div>
    </template>
   </div>
  </main>
  <UiDialog :open="!!siteConfirmation" title="Удалить сайт?" description="Сайт и его содержимое будут удалены без возможности восстановления." :busy="siteDeleting" @close="siteConfirmation=null">
   <p v-if="siteConfirmation"><strong>{{ siteConfirmation.name }}</strong><br>{{ siteConfirmation.url }}</p><UiNotice v-if="siteDialogError" tone="error">{{ siteDialogError }}</UiNotice>
   <template #footer><UiButton :disabled="siteDeleting" @click="siteConfirmation=null">Отмена</UiButton><UiButton variant="danger" :loading="siteDeleting" @click="deleteSite">Удалить</UiButton></template>
  </UiDialog>
  <KeyCreateDialog :open="createOpen" :busy="createBusy" :error="dialogError" @close="createOpen=false" @submit="create"/>
  <UiDialog :open="!!confirmation" :title="confirmation?.action==='delete'?'Удалить ключ?':'Отозвать ключ?'" :description="confirmation?.action==='delete'?'Ключ будет удалён без возможности восстановления. Если он активен, агент сразу потеряет доступ.':'Доступ агента прекратится немедленно.'" :busy="confirmBusy" @close="confirmation=null">
   <p v-if="confirmation">Ключ: <strong>{{ confirmation.key.name }}</strong></p><UiNotice v-if="dialogError" tone="error">{{ dialogError }}</UiNotice>
   <template #footer><UiButton :disabled="confirmBusy" @click="confirmation=null">Отмена</UiButton><UiButton variant="danger" :loading="confirmBusy" @click="perform">{{ confirmation?.action==='delete'?'Удалить':'Отозвать' }}</UiButton></template>
  </UiDialog>
 </div>
</template>
