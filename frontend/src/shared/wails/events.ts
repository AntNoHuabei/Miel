import { Events } from '@wailsio/runtime'
import { useEffect } from 'react'

export type EventUnsubscribe = () => void

export function eventData<T>(payload: { data?: T } | T): T {
  if (payload && typeof payload === 'object' && 'data' in payload) {
    return (payload as { data: T }).data
  }
  return payload as T
}

export function parseEventData<T>(value: unknown): T | null {
  try {
    return (typeof value === 'string' ? JSON.parse(value) : value) as T
  } catch {
    return null
  }
}

export function subscribeWailsEvent<T>(name: string, handler: (data: T) => void): EventUnsubscribe {
  const off = Events.On(name, (payload: { data?: T }) => handler(eventData<T>(payload)))
  return typeof off === 'function' ? off : () => undefined
}

export function useWailsEvent<T = unknown>(
  name: string,
  handler: (data: T) => void,
  deps: unknown[] = [],
) {
  useEffect(() => subscribeWailsEvent(name, handler), deps)
}
