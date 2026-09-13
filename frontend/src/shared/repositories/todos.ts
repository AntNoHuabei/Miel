import type { TodoLite, TodoSourceLite, TodoStatsLite } from '../types/todos'
import { Services } from './bindings'
import { normalizeList } from './helpers'

export const todoRepository = {
  list: async () => normalizeList(await Services.TodoService.ListTodos()) as TodoLite[],
  stats: async () => (await Services.TodoService.TodoStats()) as TodoStatsLite,
  create: (input: Parameters<typeof Services.TodoService.CreateTodo>[0]) => Services.TodoService.CreateTodo(input),
  update: (input: Parameters<typeof Services.TodoService.UpdateTodo>[0]) => Services.TodoService.UpdateTodo(input),
  setStatus: (id: number, status: string) => Services.TodoService.SetTodoStatus(id, status),
  delete: (id: number) => Services.TodoService.DeleteTodo(id),
  source: async (id: number) => (await Services.TodoService.GetTodoSource(id)) as TodoSourceLite,
}
