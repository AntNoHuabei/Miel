import { useCallback, useEffect, useState } from 'react'
import { App as AntApp } from 'antd'
import type { ArtifactRefLite } from '../../../shared/types/artifacts'
import { artifactRepository } from '../../../shared/repositories'
import { useWailsEvent } from '../../../shared/wails/events'

export function useArtifacts() {
  const { message } = AntApp.useApp()
  const [items, setItems] = useState<ArtifactRefLite[]>([])
  const [search, setSearch] = useState('')
  const [kind, setKind] = useState('')
  const [deleted, setDeleted] = useState(false)
  const [loading, setLoading] = useState(false)
  const reload = useCallback(async () => {
    setLoading(true)
    try { setItems(await artifactRepository.list(search, kind, deleted)) }
    catch (error) { message.error(`加载产物失败：${String(error)}`) }
    finally { setLoading(false) }
  }, [deleted, kind, message, search])
  useEffect(() => { void reload() }, [reload])
  useWailsEvent<string>('artifacts.changed', useCallback(() => void reload(), [reload]))
  const importFile = async () => { try { if (await artifactRepository.importFile()) await reload() } catch (error) { message.error(`导入失败：${String(error)}`) } }
  return { items, search, setSearch, kind, setKind, deleted, setDeleted, loading, reload, importFile }
}
