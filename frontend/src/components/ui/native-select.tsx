import * as React from 'react'

import {cn} from '@/lib/utils'

// Styled native <select>. Same look as the shadcn Input, without a Radix dependency.
function NativeSelect({className, ...props}: React.ComponentProps<'select'>) {
  return (
    <select
      data-slot="native-select"
      className={cn(
        'border-input bg-transparent dark:bg-input/30 focus-visible:border-ring focus-visible:ring-ring/50 flex h-9 rounded-md border px-3 py-1 text-sm shadow-xs outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    />
  )
}

export {NativeSelect}
