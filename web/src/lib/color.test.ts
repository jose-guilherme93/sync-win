import { afterEach, describe, expect, it, vi } from 'vitest'
import { resolveCssColor } from './color'

afterEach(() => vi.restoreAllMocks())

describe('resolveCssColor', () => {
  it('passes a concrete colour through untouched', () => {
    expect(resolveCssColor('#60a5fa')).toBe('#60a5fa')
    expect(resolveCssColor('rgba(1,2,3,0.5)')).toBe('rgba(1,2,3,0.5)')
  })

  it('resolves a custom property from the document root', () => {
    vi.spyOn(window, 'getComputedStyle').mockReturnValue({
      getPropertyValue: (name: string) => (name === '--series-1' ? '#60a5fa' : '')
    } as unknown as CSSStyleDeclaration)
    expect(resolveCssColor('var(--series-1)')).toBe('#60a5fa')
  })

  it('falls back when the property is not defined', () => {
    vi.spyOn(window, 'getComputedStyle').mockReturnValue({
      getPropertyValue: () => ''
    } as unknown as CSSStyleDeclaration)
    expect(resolveCssColor('var(--nope, #ff0000)')).toBe('#ff0000')
    expect(resolveCssColor('var(--nope)')).toBe('#34d399')
  })
})
