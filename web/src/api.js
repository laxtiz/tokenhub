const TOKEN_KEY = 'tokenhub_token'

export function getToken() { return localStorage.getItem(TOKEN_KEY) || '' }
export function setToken(t) { localStorage.setItem(TOKEN_KEY, t) }
export function clearToken() { localStorage.removeItem(TOKEN_KEY) }

export async function api(method, url, body) {
  const headers = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) headers['Authorization'] = 'Bearer ' + token
  const resp = await fetch(url, {
    method, headers,
    body: body === undefined ? undefined : JSON.stringify(body)
  })
  if (resp.status === 401) {
    clearToken()
    location.href = '/#/login'
    throw new Error('未登录')
  }
  const data = await resp.json().catch(() => null)
  if (!resp.ok) {
    const msg = data && data.error ? (typeof data.error === 'string' ? data.error : data.error.message || JSON.stringify(data.error)) : resp.statusText
    throw new Error(msg)
  }
  return data
}

export const get = (url) => api('GET', url)
export const post = (url, body) => api('POST', url, body)
export const patch = (url, body) => api('PATCH', url, body)
export const del = (url) => api('DELETE', url)
