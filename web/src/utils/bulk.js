import { api } from '../api/client'

/** Deletions are sent in bounded batches so a very large selection (tens of
 *  thousands of empty files) never turns into one oversized request that the
 *  browser or the server refuses to handle. */
export const DELETE_BATCH_SIZE = 500

/** Above this count the confirmation dialog summarizes locally instead of
 *  asking the server for a full preview (which would echo every path back). */
export const PREVIEW_LIMIT = 2000

/** Hard cap for "select all results"; the server itself caps a scan result at
 *  FILE_CLEANER_MAX_RESULTS (500000 by default). */
export const SELECT_ALL_LIMIT = 200000

/** Page size used while walking a scan result or a filtered listing. */
export const SELECT_PAGE_SIZE = 2000

/** Paths must be deleted deepest-first so recursive empty-directory cleanups
 *  work in a single pass. */
export function sortForDelete(list) {
  return [...list]
    .sort((a, b) => b.path.split('/').length - a.path.split('/').length)
    .map((e) => e.path)
}

/** Local estimate used when the selection is too large for a server preview. */
export function localPreview(list) {
  return {
    count: list.length,
    totalSize: list.reduce((s, e) => s + (e.size || 0), 0),
    fileCount: list.filter((e) => !e.isDir).length,
    dirCount: list.filter((e) => e.isDir).length,
    paths: [],
    skipped: [],
    approximate: true
  }
}

export async function deleteInBatches(paths, keepRoots, taskId, onProgress) {
  const deleted = []
  const failed = []
  let freedBytes = 0

  for (let i = 0; i < paths.length; i += DELETE_BATCH_SIZE) {
    const chunk = paths.slice(i, i + DELETE_BATCH_SIZE)
    const res = await api.deleteFiles(chunk, keepRoots, taskId)
    deleted.push(...(res.deleted || []))
    failed.push(...(res.failed || []))
    freedBytes += res.freedBytes || 0
    if (onProgress) onProgress(deleted.length + failed.length, paths.length)
  }

  return { count: deleted.length, deleted, failed, freedBytes }
}
