import { describe, expect, it } from 'vitest'
import { ApiError } from '../api/client.ts'
import { errorMessage } from '../api/errorMessage.ts'
import { t } from './index.ts'

describe('t', () => {
  it('looks up nested keys', () => {
    expect(t('nav.home')).toBe('Главная')
  })

  it('interpolates params and keeps unknown placeholders', () => {
    expect(t('home.welcome', { name: 'Анна' })).toBe('Здравствуйте, Анна!')
    expect(t('home.welcome')).toBe('Здравствуйте, {name}!')
  })
})

describe('errorMessage', () => {
  it('localizes known API error codes and appends the server message', () => {
    expect(errorMessage(new ApiError(409, 'in_use', 'referenced by rooms'))).toBe(
      'Запись используется в других данных и не может быть удалена: referenced by rooms',
    )
  })

  it('falls back to the server message for unknown codes', () => {
    expect(errorMessage(new ApiError(418, 'teapot', 'short and stout'))).toBe('short and stout')
  })

  it('reports network failures', () => {
    expect(errorMessage(new TypeError('Failed to fetch'))).toBe('Сервер недоступен')
  })
})
