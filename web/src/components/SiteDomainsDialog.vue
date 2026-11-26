<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { Domain, Site } from './types'
import UiButton from './ui/UiButton.vue'
import UiDialog from './ui/UiDialog.vue'
import UiField from './ui/UiField.vue'
import UiInput from './ui/UiInput.vue'
import UiNotice from './ui/UiNotice.vue'

type Api = (path: string, method?: string, body?: unknown) => Promise<unknown>
type DomainListResponse = { domains: Domain[] }
type DomainResponse = { domain: Domain }
type ApiError = { code?: string; message?: string }

const props = defineProps<{
  open: boolean
  site: Site | null
  api: Api
  sessionGeneration: number
}>()
const emit = defineEmits<{ close: [] }>()

const domains = ref<Domain[]>([])
const hostname = ref('')
const loading = ref(false)
const submitting = ref(false)
const error = ref('')
const busy = ref<Record<string, boolean>>({})
const disabled = ref(false)
const sortedDomains = computed(() => domains.value)

const states: Record<string, string> = {
  pending_dns: 'Ожидает настройки DNS',
  provisioning_tls: 'Настраивается HTTPS',
  active: 'Активен',
  suspended: 'Приостановлен',
}
const errors: Record<string, string> = {
  invalid_domain: 'Некорректное доменное имя',
  invalid_request: 'Проверьте введённое значение',
  site_not_found: 'Сайт не найден',
  domain_not_found: 'Домен не найден',
  domain_conflict: 'Это имя уже закреплено за другим сайтом',
  reserved_domain: 'Это имя зарезервировано',
  domains_disabled: 'Управление доменами отключено',
  dns_lookup_failed: 'Не удалось выполнить DNS-проверку. Проверка будет повторена автоматически.',
  dns_mismatch: 'DNS-записи не совпадают с ожидаемыми. Проверьте настройки DNS.',
  tls_unavailable: 'HTTPS-сертификат пока недоступен. Проверка будет повторена автоматически.',
  https_probe_failed: 'Проверка HTTPS не пройдена. Убедитесь, что домен ведёт на этот сайт.',
}

let timer: ReturnType<typeof setInterval> | undefined
let requestGeneration = 0
let requestBusy = false
let domainRevision = 0

function message(cause: unknown): string {
  const value = cause as ApiError
  if (value?.code === 'domains_disabled') disabled.value = true
  return errors[value?.code ?? ''] ?? value?.message ?? 'Не удалось выполнить запрос'
}

function stop(): void {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
  requestGeneration++
  loading.value = false
}

function isCurrent(generation: number, siteID: string, session: number): boolean {
  return generation === requestGeneration
    && props.open
    && props.site?.id === siteID
    && props.sessionGeneration === session
}

// The browser URL API encodes IDNs but does not expose punycode decoding.
function decodePunycode(label: string): string {
  if (!label.startsWith('xn--')) return label

  const input = label.slice(4)
  const output: number[] = []
  const delimiter = input.lastIndexOf('-')
  let cursor = 0
  if (delimiter >= 0) {
    for (const character of input.slice(0, delimiter)) {
      output.push(character.charCodeAt(0))
    }
    cursor = delimiter + 1
  }

  let codePoint = 128
  let index = 0
  let bias = 72
  while (cursor < input.length) {
    const oldIndex = index
    let weight = 1
    for (let k = 36; ; k += 36) {
      const character = input.codePointAt(cursor++)
      if (character === undefined) throw new Error('Invalid punycode')
      const digit = character >= 48 && character <= 57
        ? character - 22
        : character >= 65 && character <= 90
          ? character - 65
          : character - 97
      if (digit < 0 || digit >= 36) throw new Error('Invalid punycode')

      index += digit * weight
      const threshold = k <= bias ? 1 : k >= bias + 26 ? 26 : k - bias
      if (digit < threshold) break
      weight *= 36 - threshold
    }

    const pointCount = output.length + 1
    let delta = index - oldIndex
    delta = Math.floor(delta / (oldIndex === 0 ? 700 : 2))
    delta += Math.floor(delta / pointCount)
    let adaptation = 0
    while (delta > 455) {
      delta = Math.floor(delta / 35)
      adaptation += 36
    }
    bias = adaptation + Math.floor(36 * delta / (delta + 38))
    codePoint += Math.floor(index / pointCount)
    index %= pointCount
    output.splice(index, 0, codePoint)
    index++
  }

  return String.fromCodePoint(...output)
}

function unicodeHostname(host: string): string {
  return host.split('.').map(decodePunycode).join('.')
}

function currentSite(): Site | null {
  return props.open ? props.site : null
}

async function load(
  generation = requestGeneration,
  siteID = props.site?.id ?? '',
  session = props.sessionGeneration,
): Promise<void> {
  if (!props.open || !siteID || requestBusy) return

  requestBusy = true
  loading.value = true
  const revision = domainRevision
  try {
    const result = await props.api(`/api/sites/${encodeURIComponent(siteID)}/domains`) as DomainListResponse
    if (isCurrent(generation, siteID, session) && revision === domainRevision) {
      domains.value = result.domains ?? []
    }
  } catch (cause) {
    if (isCurrent(generation, siteID, session)) {
      if ((cause as ApiError)?.code === 'site_not_found') emit('close')
      else error.value = message(cause)
    }
  } finally {
    requestBusy = false
    if (isCurrent(generation, siteID, session)) loading.value = false
  }
}

function start(): void {
  stop()
  domains.value = []
  hostname.value = ''
  error.value = ''
  disabled.value = false
  if (!props.open || !props.site) return

  const generation = requestGeneration
  const siteID = props.site.id
  const session = props.sessionGeneration
  void load(generation, siteID, session)
  timer = setInterval(() => void load(generation, siteID, session), 5000)
}

watch(
  () => [props.open, props.site?.id, props.sessionGeneration],
  () => {
    if (!props.open) stop()
    else start()
  },
  { immediate: true },
)
onBeforeUnmount(stop)

async function add(): Promise<void> {
  const site = currentSite()
  if (!site || submitting.value) return

  const generation = requestGeneration
  const session = props.sessionGeneration
  submitting.value = true
  error.value = ''
  try {
    const result = await props.api(
      `/api/sites/${encodeURIComponent(site.id)}/domains`,
      'POST',
      { hostname: hostname.value },
    ) as DomainResponse
    if (isCurrent(generation, site.id, session)) {
      domainRevision++
      domains.value = [result.domain, ...domains.value.filter(domain => domain.id !== result.domain.id)]
      hostname.value = ''
    }
  } catch (cause) {
    if (isCurrent(generation, site.id, session)) error.value = message(cause)
  } finally {
    submitting.value = false
  }
}

async function check(domain: Domain): Promise<void> {
  if (busy.value[domain.id]) return
  const site = currentSite()
  if (!site) return

  const generation = requestGeneration
  const session = props.sessionGeneration
  busy.value[domain.id] = true
  error.value = ''
  try {
    const result = await props.api(
      `/api/sites/${encodeURIComponent(site.id)}/domains/${encodeURIComponent(domain.id)}/check`,
      'POST',
    ) as DomainResponse
    if (isCurrent(generation, site.id, session)) {
      domainRevision++
      domains.value = domains.value.map(item => item.id === domain.id ? result.domain : item)
    }
  } catch (cause) {
    if (isCurrent(generation, site.id, session)) error.value = message(cause)
  } finally {
    delete busy.value[domain.id]
  }
}

async function remove(domain: Domain): Promise<void> {
  if (!window.confirm(`Отвязать домен ${domain.hostname}?`)) return
  const site = currentSite()
  if (!site) return

  const generation = requestGeneration
  const session = props.sessionGeneration
  busy.value[domain.id] = true
  error.value = ''
  try {
    await props.api(
      `/api/sites/${encodeURIComponent(site.id)}/domains/${encodeURIComponent(domain.id)}`,
      'DELETE',
    )
    if (isCurrent(generation, site.id, session)) {
      domainRevision++
      domains.value = domains.value.filter(item => item.id !== domain.id)
    }
  } catch (cause) {
    if (isCurrent(generation, site.id, session)) error.value = message(cause)
  } finally {
    delete busy.value[domain.id]
  }
}

function date(value: number | null): string {
  return value ? new Date(value * 1000).toLocaleString() : '—'
}
</script>
<template>
  <UiDialog :open="open" title="Домены сайта" :description="site?.name" @close="emit('close')">
    <UiNotice v-if="disabled" tone="info">
      Управление собственными доменами отключено оператором.
    </UiNotice>
    <template v-else>
      <UiNotice v-if="error" tone="error" role="alert">{{ error }}</UiNotice>
      <div class="grid gap-2 text-sm text-muted">
        <p>
          Проверяется только настройка DNS. Отдельной проверки владения через TXT-запись нет:
          совпадение IP-адресов не доказывает владение доменом.
        </p>
        <p>
          Для домена верхнего уровня (apex) DNS-провайдер может не поддерживать CNAME.
          Настройте записи, поддерживаемые вашим провайдером.
        </p>
      </div>
      <form class="grid gap-3" @submit.prevent="add">
        <UiField v-slot="field" id="custom-domain" label="Домен">
          <UiInput
            :id="field.id"
            v-model="hostname"
            :disabled="submitting"
            :aria-describedby="field.describedBy"
            autocomplete="url"
            placeholder="example.com"
          />
        </UiField>
        <UiButton variant="primary" type="submit" :loading="submitting" :disabled="!hostname.trim()">
          Добавить
        </UiButton>
      </form>
      <div
        v-if="!sortedDomains.length && !loading"
        class="rounded-control border border-border p-4 text-sm text-muted"
      >
        Собственные домены ещё не добавлены.
      </div>
      <div v-if="loading && !sortedDomains.length" class="text-sm text-muted" aria-live="polite">
        Загрузка доменов…
      </div>
      <article
        v-for="domain in sortedDomains"
        :key="domain.id"
        class="grid gap-3 rounded-card border border-border p-4 [overflow-wrap:anywhere]"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div>
            <strong>{{ unicodeHostname(domain.hostname) }}</strong>
            <p v-if="unicodeHostname(domain.hostname) !== domain.hostname" class="text-xs text-muted">
              ASCII: {{ domain.hostname }}
            </p>
          </div>
          <span class="text-sm">{{ states[domain.state] || domain.state }}</span>
        </div>
        <a
          v-if="domain.state === 'active'"
          :href="domain.url"
          target="_blank"
          rel="noopener noreferrer"
          class="text-accent"
        >{{ domain.url }}</a>
        <p v-if="domain.error_code" class="text-sm text-danger">
          {{ errors[domain.error_code] || `Ошибка проверки: ${domain.error_code}` }}
        </p>
        <div class="grid gap-1 text-xs text-muted">
          <span>DNS-проверка: {{ date(domain.dns_checked_at) }} · HTTPS-проверка: {{ date(domain.https_checked_at) }}</span>
          <span>Следующая проверка: {{ date(domain.next_check_at) }}</span>
        </div>
        <div v-if="domain.dns_records.length" class="grid gap-2">
          <strong class="text-sm">Настройки DNS</strong>
          <div
            v-for="(record, index) in domain.dns_records"
            :key="`${record.type}-${record.name}-${index}`"
            class="grid gap-1 border-l-2 border-border pl-3 text-sm"
          >
            <span>{{ record.type }} · {{ record.name }}</span>
            <code class="[overflow-wrap:anywhere]">{{ record.value }}</code>
          </div>
        </div>
        <div class="flex flex-wrap gap-2">
          <UiButton :disabled="!!busy[domain.id]" @click="check(domain)">
            {{ busy[domain.id] ? 'Проверка…' : 'Проверить снова' }}
          </UiButton>
          <UiButton variant="ghost" :disabled="!!busy[domain.id]" @click="remove(domain)">
            Отвязать
          </UiButton>
        </div>
      </article>
    </template>
  </UiDialog>
</template>
