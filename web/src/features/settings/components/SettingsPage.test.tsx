import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import SettingsPage from './SettingsPage'

describe('SettingsPage', () => {
  it('shows provider and model in the status banner', () => {
    render(
      <SettingsPage
        vm={
          {
            loading: false,
            status: {
              configured: true,
              provider_name: 'gemini-oauth-pool',
              model: 'gemini-2.5-pro',
            },
            runtimeMetrics: null,
            message: null,
          } as any
        }
      />,
    )

    expect(
      screen.getByText('Provider active: gemini-oauth-pool / gemini-2.5-pro'),
    ).toBeTruthy()
  })
})
