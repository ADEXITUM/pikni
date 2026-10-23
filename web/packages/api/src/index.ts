import createClient from 'openapi-fetch'

import type { components, paths } from './pikni.gen'

export type { Middleware } from 'openapi-fetch'

export type Location = components['schemas']['Location']
export type LocationKind = components['schemas']['LocationKind']
export type Me = components['schemas']['Me']
export type Permission = components['schemas']['Permission']
export type Problem = components['schemas']['Problem']
export type Role = components['schemas']['Role']

export const api = createClient<paths>({ baseUrl: '/api/v1' })

export class ApiError extends Error {
  constructor(readonly problem: Problem) {
    super(problem.detail ?? problem.title)
    this.name = 'ApiError'
  }
}
