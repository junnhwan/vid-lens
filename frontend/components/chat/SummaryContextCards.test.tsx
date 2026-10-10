// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { parseSummaryAnnotations } from '@/lib/summaryAnnotations'
import { SummaryContextCards } from './SummaryContextCards'

afterEach(cleanup)
const ref = {kind:'summary_screenshot' as const,task_id:1,version_ref:{generated_version:2},document_digest:'digest',block_id:'chapter',block_digest:'block',text_start:0,text_end:2,quote:'图注',screenshot_ref:'opaque',source_title:'原视频',block_title:'安装',provenance:'derived_summary'}
test('nullable frozen annotations render the saved quote with caption-only provenance', () => {
  expect(parseSummaryAnnotations(null)).toEqual([])
  expect(parseSummaryAnnotations('invalid')).toEqual([])
  const accepted=parseSummaryAnnotations(JSON.stringify([ref]))
  render(<SummaryContextCards refs={accepted} saved fallbackTitle="当前改名的视频" />)
  expect(screen.getByText('原视频')).toBeTruthy()
  expect(screen.getByText('图注')).toBeTruthy()
  expect(screen.getByText('已验证图注，未输入原图')).toBeTruthy()
  expect(screen.getByText('生成版本 2 · 已保存快照')).toBeTruthy()
  expect(document.querySelector('img')).toBeNull()
})
test('draft attachment removal leaves the question as separate input', () => {
  const remove=vi.fn()
  render(<SummaryContextCards refs={[ref]} fallbackTitle="视频" onRemove={remove} />)
  fireEvent.click(screen.getByRole('button',{name:'移除摘要选段 1'}))
  expect(remove).toHaveBeenCalledWith(0)
})
