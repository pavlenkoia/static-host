<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
const router = useRouter()
type Key = { id: string; name: string; created_at: number; expires_at: number; status: string }
const sessionUnavailable = ref(false)
watch(() => router.currentRoute.value.path, () => { token.value = '' })
const user = ref(''), csrf = ref(''), login = ref(''), password = ref(''), name = ref(''), days = ref(90), token = ref(''), error = ref(''), loading = ref(true), busy = ref(false), keys = ref<Key[]>([]), confirm = ref('')
async function api(path: string, method = 'GET', body?: unknown) {
 const headers: Record<string,string> = {}
 if (body !== undefined) headers['Content-Type'] = 'application/json'
 if (method !== 'GET' && csrf.value) headers['X-CSRF-Token'] = csrf.value
 const response = await fetch(path, { method, headers, credentials: 'same-origin', ...(body !== undefined ? { body: JSON.stringify(body) } : {}) })
 if (!response.ok) { if (response.status === 401 && path !== '/api/login') { user.value = ''; csrf.value = ''; token.value = ''; keys.value = []; await router.replace('/login') }; throw new Error(response.status === 401 && path === '/api/login' ? 'Неверный логин или пароль' : `Ошибка сервиса (${response.status})`) }
 return response.status === 204 ? null : response.json()
}
async function refresh() { loading.value = true; sessionUnavailable.value = false; error.value = ''; try { const s = await api('/api/session'); user.value = s.login; csrf.value = s.csrf_token; await router.replace('/'); keys.value = (await api('/api/keys')).keys } catch (e) { if (user.value || !(e instanceof Error && e.message.includes('401'))) { sessionUnavailable.value = !user.value; error.value = String(e instanceof Error ? e.message : e) } } finally { loading.value = false } }
async function signIn() { busy.value = true; error.value = ''; try { const s = await api('/api/login', 'POST', { login: login.value, password: password.value }); user.value = s.login; csrf.value = s.csrf_token; await router.replace('/'); keys.value = (await api('/api/keys')).keys } catch(e) { error.value = String(e instanceof Error ? e.message : e) } finally { password.value = ''; busy.value = false } }
async function signOut() { busy.value = true; error.value = ''; try { await api('/api/logout', 'POST'); user.value = ''; csrf.value = ''; token.value = ''; keys.value = []; await router.replace('/login') } catch(e) { error.value = String(e instanceof Error ? e.message : e) } finally { busy.value = false } }
async function createKey() { busy.value = true; error.value = ''; try { const result = await api('/api/keys', 'POST', { name: name.value, expires_in_days: days.value }); token.value = result.token; name.value = ''; keys.value = (await api('/api/keys')).keys } catch(e) { error.value = String(e instanceof Error ? e.message : e) } finally { busy.value = false } }
async function revoke(id: string) { busy.value = true; error.value = ''; try { await api(`/api/keys/${encodeURIComponent(id)}/revoke`, 'POST'); confirm.value = ''; keys.value = (await api('/api/keys')).keys } catch(e) { error.value = String(e instanceof Error ? e.message : e) } finally { busy.value = false } }
onMounted(refresh)
</script>
<template>
 <main class="mx-auto max-w-4xl p-5 text-slate-900">
  <h1 class="text-2xl font-semibold mb-6">Кабинет владельца</h1>
  <p v-if="loading">Загрузка…</p>
  <template v-else>
   <p v-if="error" role="alert" class="mb-4 text-red-700">{{ error }} <button v-if="!user && error !== 'Неверный логин или пароль'" @click="refresh" class="underline">Повторить</button></p>
   <form v-if="!user && !sessionUnavailable" @submit.prevent="signIn" class="grid gap-4 max-w-sm">
    <label>Логин<input v-model="login" autocomplete="username" required class="block w-full border rounded p-2"></label>
    <label>Пароль<input v-model="password" type="password" autocomplete="current-password" required class="block w-full border rounded p-2"></label>
    <button :disabled="busy" class="bg-slate-900 text-white rounded p-2 disabled:opacity-50">{{ busy ? 'Вход…' : 'Войти' }}</button>
   </form>
   <template v-else-if="user">
    <div class="flex justify-between gap-4 mb-6"><span>{{ user }}</span><button :disabled="busy" @click="signOut" class="underline">Выйти</button></div>
    <section v-if="token" class="border rounded p-4 mb-6 break-all"><p class="mb-2">Сохраните ключ: он больше не будет показан.</p><input :value="token" readonly class="border p-2 w-full"><button @click="token = ''" class="underline mt-2">Закрыть</button></section>
    <form @submit.prevent="createKey" class="grid gap-3 mb-8"><h2 class="text-xl">Создать ключ агента</h2><label>Название<input v-model="name" required maxlength="160" class="block border rounded p-2 w-full"></label><label>Срок в днях<input v-model.number="days" type="number" min="1" max="365" required class="block border rounded p-2"></label><button :disabled="busy" class="bg-slate-900 text-white rounded p-2 disabled:opacity-50">Создать</button></form>
    <h2 class="text-xl mb-3">Ключи</h2><p v-if="!keys.length">Ключей пока нет.</p><ul class="grid gap-3"><li v-for="key in keys" :key="key.id" class="border rounded p-4 break-all"><p class="font-semibold">{{ key.name }}</p><p>ID: {{ key.id }}</p><p>Создан: {{ new Date(key.created_at * 1000).toLocaleString() }}</p><p>Истекает: {{ new Date(key.expires_at * 1000).toLocaleString() }}</p><p>Статус: {{ key.status }}</p><template v-if="key.status !== 'revoked'"><button v-if="confirm !== key.id" @click="confirm = key.id" :disabled="busy" class="underline">Отозвать</button><div v-else class="flex gap-4"><span>Отозвать ключ?</span><button @click="confirm = ''" :disabled="busy" class="underline">Отмена</button><button @click="revoke(key.id)" :disabled="busy" class="underline">Отозвать</button></div></template></li></ul>
   </template>
  </template>
 </main>
</template>
