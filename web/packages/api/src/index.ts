import createClient from 'openapi-fetch'

import type { components, paths } from './pikni.gen'

export type Location = components['schemas']['Location']
export type LocationKind = components['schemas']['LocationKind']
export type Problem = components['schemas']['Problem']

export const api = createClient<paths>({ baseUrl: '/api/v1' })

export class ApiError extends Error {
  constructor(readonly problem: Problem) {
    super(problem.detail ?? problem.title)
    this.name = 'ApiError'
  }
}
