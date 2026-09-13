export interface ExtractedTodoLite { title: string; description: string; milestone: boolean; dueDate: string }
export interface ClipboardTodoDraftLite { draftId: string; kind: string; text: string; dataUri: string; createdAt: number; items: ExtractedTodoLite[] }
