import type { VariantProps } from "class-variance-authority"
import { cva } from "class-variance-authority"

export { default as Button } from "./Button.vue"

export const buttonVariants = cva(
  "btn-press inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-2xl text-base font-medium transition-all duration-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-6 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        // Clean solid primary — flat fill, soft shadow, no gradient/glow
        default: "bg-primary text-primary-foreground shadow-sm hover:bg-emerald-600 hover:shadow-md dark:hover:bg-emerald-400",
        destructive:
          "bg-destructive text-destructive-foreground shadow-sm hover:bg-red-600 hover:shadow-md",
        // Crisp outline, clean in light mode
        outline:
          "border border-border bg-background text-foreground hover:bg-muted hover:border-foreground/20",
        // Active/selected state - works in both dark and light modes
        active:
          "border border-primary/50 bg-primary text-primary-foreground",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-muted",
        // Subtle ghost with hover
        ghost: "text-muted-foreground hover:bg-muted hover:text-foreground",
        link: "text-primary underline-offset-4 hover:underline",
        // Glass variant for cards/panels
        glass: "bg-white/[0.04] border border-white/[0.08] text-foreground hover:bg-white/[0.08] light:bg-gray-50 light:border-gray-200 light:hover:bg-gray-100",
      },
      size: {
        "default": "h-12 px-6 py-3",
        "xs": "h-9 rounded-xl px-3 text-sm [&_svg]:size-5",
        "sm": "h-10 rounded-xl px-4 text-sm [&_svg]:size-5",
        "lg": "h-14 rounded-2xl px-9 text-lg [&_svg]:size-7",
        "icon": "h-12 w-12",
        "icon-sm": "size-10 [&_svg]:size-5",
        "icon-lg": "size-14 [&_svg]:size-7",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
)

export type ButtonVariants = VariantProps<typeof buttonVariants>
