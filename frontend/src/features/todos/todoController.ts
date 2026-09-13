import { todoRepository } from '../../shared/repositories'
import { todoStore } from './todoStore'

let activeLoad: Promise<void> | null = null

export function reloadTodos() {
  if (activeLoad) return activeLoad
  todoStore.getState().beginLoad()
  activeLoad = Promise.all([todoRepository.list(), todoRepository.stats()])
    .then(([items, stats]) => todoStore.getState().setData(items, stats))
    .catch((error) => todoStore.getState().setError(String(error)))
    .finally(() => { activeLoad = null })
  return activeLoad
}

export function refreshTodosIfLoaded() {
  if (todoStore.getState().loaded) void reloadTodos()
}

export async function saveTodo(input: Parameters<typeof todoRepository.create>[0], editing: boolean) {
  if (editing) await todoRepository.update(input)
  else await todoRepository.create(input)
  await reloadTodos()
}

export async function toggleTodoStatus(id: number, currentStatus: string) {
  await todoRepository.setStatus(id, currentStatus === 'done' ? 'pending' : 'done')
  await reloadTodos()
}

export async function deleteTodo(id: number) {
  await todoRepository.delete(id)
  await reloadTodos()
}

export function loadTodoSource(sourceId: number) {
  return todoRepository.source(sourceId)
}
