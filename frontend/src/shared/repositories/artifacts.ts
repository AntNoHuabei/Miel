import type { ArtifactRefLite, ArtifactPreviewLite, SaveMessageArtifactInput } from '../types/artifacts'
import { Services } from './bindings'
import { normalizeList } from './helpers'

export const artifactRepository = {
  list: async (search = '', kind = '', deleted = false) => normalizeList(await Services.ArtifactService.List(search, kind, deleted)) as ArtifactRefLite[],
  versions: async (id: string) => normalizeList(await Services.ArtifactService.Versions(id)) as ArtifactRefLite[],
  preview: async (id: string, version: number) => await Services.ArtifactService.Preview(id, version) as ArtifactPreviewLite,
  importFile: async () => await Services.ArtifactService.Import() as ArtifactRefLite | null,
  exportFile: (id: string, version: number) => Services.ArtifactService.Export(id, version),
  open: (id: string, version: number) => Services.ArtifactService.Open(id, version),
  delete: (id: string) => Services.ArtifactService.Delete(id),
  restore: (id: string) => Services.ArtifactService.Restore(id),
  saveMessage: (input: SaveMessageArtifactInput) => Services.ArtifactService.SaveMessage(input) as Promise<ArtifactRefLite>,
}
