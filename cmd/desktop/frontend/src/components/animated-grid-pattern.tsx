import { motion } from 'framer-motion'
import { cn } from '@/lib/utils'

interface AnimatedGridPatternProps {
  width?: number
  height?: number
  x?: number
  y?: number
  strokeDasharray?: string | number
  numSquares?: number
  className?: string
  maxOpacity?: number
  duration?: number
  repeatDelay?: number
}

export function AnimatedGridPattern({
  width = 40,
  height = 40,
  x = -1,
  y = -1,
  strokeDasharray = 0,
  numSquares = 50,
  className,
  maxOpacity = 0.5,
  duration = 4,
  repeatDelay = 0.5,
}: AnimatedGridPatternProps) {
  const squares = Array.from({ length: numSquares }, () => ({
    x: Math.floor(Math.random() * 30) - 5,
    y: Math.floor(Math.random() * 20) - 5,
    delay: Math.random() * 5,
  }))

  return (
    <motion.svg
      aria-hidden="true"
      className={cn(
        'pointer-events-none absolute inset-0 h-full w-full',
        'fill-muted-foreground/15 stroke-muted-foreground/30',
        className
      )}
      animate={{ opacity: [0.3, 0.8, 0.3] }}
      transition={{ duration: 8, repeat: Infinity, ease: 'easeInOut' }}
    >
      <defs>
        <pattern id="grid-pattern" width={width} height={height} patternUnits="userSpaceOnUse" x={x} y={y}>
          <path
            d={`M ${width} 0 L 0 0 0 ${height}`}
            fill="none"
            stroke="currentColor"
            strokeWidth="0.5"
            strokeDasharray={strokeDasharray}
          />
        </pattern>
      </defs>
      <rect width="100%" height="100%" fill="url(#grid-pattern)" />
      {squares.map((square) => (
        <motion.rect
          key={`${square.x}-${square.y}-${square.delay}`}
          x={square.x * width}
          y={square.y * height}
          width={width}
          height={height}
          stroke="currentColor"
          fill="currentColor"
          initial={{ opacity: 0 }}
          animate={{
            opacity: [0, maxOpacity, 0],
            scale: [1, 1.2, 1],
          }}
          transition={{
            duration,
            delay: square.delay,
            repeat: Infinity,
            repeatDelay,
            ease: 'easeInOut',
          }}
        />
      ))}
    </motion.svg>
  )
}
