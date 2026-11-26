import { expect, test } from '@playwright/test'

test.use({ baseURL: process.env.E2E_BASE_URL })

test('manage a custom domain in the UI and MCP', async ({ page, request }) => {
  let keyID = '', siteID = '', keyToken = ''
  try {
    await page.goto('/login')
    await page.getByLabel('Логин').fill('admin')
    await page.getByLabel('Пароль').fill('e2e-test-password')
    await page.getByRole('button', { name: 'Войти' }).click()
    await expect(page.getByRole('heading', { name: 'Сайты' })).toBeVisible()
    const session = await (await page.request.get('/api/session')).json()
    const origin = new URL(page.url()).origin
    const headers = { Origin: origin, 'X-CSRF-Token': session.csrf_token }
    const keyResponse = await page.request.post('/api/keys', { headers, data: { name: 'Domains E2E', expires_in_days: null } })
    expect(keyResponse.status()).toBe(201)
    const key = await keyResponse.json(); keyID = key.key.id; keyToken = key.token
    const mcpHeaders = { Authorization: `Bearer ${keyToken}`, Accept: 'application/json, text/event-stream', 'MCP-Protocol-Version': '2025-11-25' }
    async function call(name:string, args:Record<string,unknown>) {
      const response = await request.post('/mcp', { headers: mcpHeaders, data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } } })
      expect(response.status()).toBe(200)
      const body = await response.json(); expect(body.result.isError).not.toBe(true); return body.result.structuredContent
    }
    const created = await call('sites_create', { name: 'Domain fixture' }); siteID = created.site.id
    await page.getByRole('button', { name: 'Обновить список' }).click()
    await page.getByRole('button', { name: 'Домены сайта Domain fixture' }).click()
    const dialog = page.getByRole('dialog', { name: 'Домены сайта' })
    await expect(dialog.getByText('Собственные домены ещё не добавлены.')).toBeVisible()
    const initialBox = await dialog.boundingBox()
    const pollHeld = Promise.withResolvers<void>()
    const pollStarted = Promise.withResolvers<void>()
    await page.route(`**/api/sites/${siteID}/domains`, async route => {
      pollStarted.resolve()
      await pollHeld.promise
      await route.continue()
    }, { times: 1 })
    await pollStarted.promise
    await expect(dialog.getByText('Собственные домены ещё не добавлены.')).toBeVisible()
    expect(await dialog.boundingBox()).toEqual(initialBox)
    const pollResponse = page.waitForResponse(response => response.request().method() === 'GET' && response.url().endsWith(`/sites/${siteID}/domains`))
    pollHeld.resolve()
    await pollResponse
    await dialog.getByRole('button', { name: 'Закрыть', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    await page.getByRole('button', { name: 'Домены сайта Domain fixture' }).click()
    await dialog.getByRole('button', { name: 'Закрыть окно', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    await page.getByRole('button', { name: 'Домены сайта Domain fixture' }).click()
    await expect(dialog.getByText('Собственные домены ещё не добавлены.')).toBeVisible()
    await page.route(`**/api/sites/${siteID}/domains`, route => route.abort())
    await dialog.getByLabel('Домен').fill('пример.invalid')
    await dialog.getByRole('button', { name: 'Добавить' }).click()
    await expect(dialog.getByRole('alert')).toBeVisible()
    await page.unroute(`**/api/sites/${siteID}/domains`)
    await dialog.getByRole('button', { name: 'Добавить' }).click()
    await expect(dialog.getByText('пример.invalid', { exact: true })).toBeVisible()
    await expect(dialog.getByText('ASCII: xn--e1afmkfd.invalid')).toBeVisible()
    await expect(dialog.getByText('Нужна настройка домена', { exact: true })).toBeVisible()
    await expect(dialog.getByText('Выберите один способ:', { exact: false })).toBeVisible()
    expect((await call('domains_list', { site_id: siteID })).domains).toHaveLength(1)
    const domain = (await call('domains_list', { site_id: siteID })).domains[0]
    expect((await page.request.delete(`/api/sites/${siteID}/domains/${domain.id}`, { headers: { Origin: origin } })).status()).toBe(403)
    const checkResponsePromise = page.waitForResponse(response =>
      response.request().method() === 'POST' && response.url().endsWith(`/domains/${domain.id}/check`),
    )
    await dialog.getByRole('button', { name: 'Проверить снова' }).click()
    const checkResponse = await checkResponsePromise
    expect(checkResponse.status()).toBe(202)
    const checkResult = await checkResponse.json()
    const nextCheck = new Date(checkResult.domain.next_check_at * 1000).toLocaleString()
    await dialog.getByText('Подробности проверки', { exact: true }).click()
    await expect(dialog.getByText(`Следующая проверка: ${nextCheck}`, { exact: true })).toBeVisible()
    await page.route(`**/api/sites/${siteID}/domains`, async route => {
      const response = await route.fetch()
      const body = await response.json()
      await route.fulfill({ response, json: { ...body, domains: body.domains.map((item: Record<string, unknown>) => ({ ...item, state: 'active', error_code: null })) } })
    }, { times: 1 })
    await expect(dialog.getByText('Домен подключён, защищённое соединение HTTPS работает. Больше ничего настраивать не нужно.')).toBeVisible({ timeout: 10000 })
    await expect(dialog.getByRole('link', { name: 'Открыть сайт', exact: false })).toBeVisible()
    await expect(dialog.getByText('Выберите один способ:', { exact: false })).not.toBeVisible()
    await page.route(`**/api/sites/${siteID}/domains`, async route => {
      const response = await route.fetch()
      const body = await response.json()
      await route.fulfill({ response, json: { ...body, domains: body.domains.map((item: Record<string, unknown>) => ({ ...item, state: 'active', error_code: null })) } })
    }, { times: 1 })
    await dialog.getByRole('button', { name: 'Закрыть', exact: true }).click()
    const customDomains = page.getByLabel('Собственные домены', { exact: true })
    await expect(customDomains.getByRole('link', { name: 'xn--e1afmkfd.invalid', exact: true })).toBeVisible()
    await expect(customDomains.getByText('Подключён', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Домены сайта Domain fixture' }).click()
    await page.setViewportSize({ width: 390, height: 600 })
    await dialog.getByText('Как настроить домен', { exact: true }).click()
    await dialog.getByRole('button', { name: 'Проверить снова' }).scrollIntoViewIfNeeded()
    await expect(dialog.getByRole('button', { name: 'Закрыть окно', exact: true })).toBeInViewport()
    await expect(dialog.getByRole('button', { name: 'Закрыть', exact: true })).toBeInViewport()
    await page.screenshot({ path: test.info().outputPath('domains-mobile.png') })
    await page.setViewportSize({ width: 1280, height: 720 })
    page.once('dialog', confirmation => confirmation.accept())
    await dialog.getByRole('button', { name: 'Отвязать' }).click()
    await expect(dialog.getByText('Собственные домены ещё не добавлены.')).toBeVisible()
    expect((await call('domains_list', { site_id: siteID })).domains).toHaveLength(0)
    const deleteSiteForPoll = await page.request.delete(`/api/sites/${siteID}`, { headers })
    expect(deleteSiteForPoll.status()).toBe(204)
    await expect(dialog).toHaveCount(0, { timeout: 10000 })
    siteID = ''

    const logoutSite = await call('sites_create', { name: 'Logout dialog fixture' })
    siteID = logoutSite.site.id
    await page.getByRole('button', { name: 'Обновить список' }).click()
    await page.getByRole('button', { name: 'Домены сайта Logout dialog fixture' }).click()
    const logoutDialog = page.getByRole('dialog', { name: 'Домены сайта' })
    await expect(logoutDialog).toBeVisible()
    const logoutResponse = await page.request.post('/api/logout', { headers })
    expect(logoutResponse.status()).toBe(204)
    await expect(logoutDialog).toHaveCount(0, { timeout: 10000 })
    await expect(page.getByLabel('Логин')).toBeVisible()

    await page.getByLabel('Логин').fill('admin')
    await page.getByLabel('Пароль').fill('e2e-test-password')
    await page.getByRole('button', { name: 'Войти' }).click()
    await expect(page.getByText('Logout dialog fixture', { exact: true })).toBeVisible()
    const cleanupSession = await (await page.request.get('/api/session')).json()
    const cleanupHeaders = { Origin: origin, 'X-CSRF-Token': cleanupSession.csrf_token }
    const deleteSiteResponse = await page.request.delete(`/api/sites/${siteID}`, { headers: cleanupHeaders })
    expect(deleteSiteResponse.status()).toBe(204)
    siteID = ''
    const deleteKeyResponse = await page.request.delete(`/api/keys/${keyID}`, { headers: cleanupHeaders })
    expect(deleteKeyResponse.status()).toBe(204)
    keyID = ''
  } finally {
    let sessionResponse = await page.request.get('/api/session')
    if (!sessionResponse.ok()) {
      await page.goto('/login')
      await page.getByLabel('Логин').fill('admin')
      await page.getByLabel('Пароль').fill('e2e-test-password')
      await page.getByRole('button', { name: 'Войти' }).click()
      await expect(page.getByText(/Domain fixture|Logout dialog fixture/)).toBeVisible()
      sessionResponse = await page.request.get('/api/session')
    }
    if (sessionResponse.ok()) {
      const session = await sessionResponse.json()
      const cleanupHeaders = { Origin: new URL(page.url()).origin, 'X-CSRF-Token': session.csrf_token }
      if (siteID) {
        const response = await page.request.delete(`/api/sites/${siteID}`, { headers: cleanupHeaders })
        if (response.status() === 204 || response.status() === 404) siteID = ''
      }
      if (keyID) {
        const response = await page.request.delete(`/api/keys/${keyID}`, { headers: cleanupHeaders })
        if (response.status() === 204 || response.status() === 404) keyID = ''
      }
    }
  }
})
