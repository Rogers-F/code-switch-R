/**
 * 页面轮询生命周期管理 Composable
 * @author sm
 * @description 供 keep-alive 缓存页面在激活期间运行轮询。核心设计：
 *   1. setTimeout 链式调度：上一轮 fn 完成后才排下一轮，天然单飞（single-flight），
 *      避免 setInterval 在 fn 耗时超过间隔时产生的请求堆叠；
 *   2. epoch（纪元）机制：start/stop/bump 均递增纪元，链上闭包持有旧纪元时
 *      自动作废，调用方也可用同样思路丢弃在途异步回填。
 */

export interface Poller {
  /** 启动轮询链；重复调用幂等（等价于先 stop 再 start）。immediate=true 时立即执行首轮 */
  start(immediate?: boolean): void
  /** 停止轮询链：递增纪元并清除待触发的调度，在途 fn 完成后不再排下一轮 */
  stop(): void
  /** 仅作废在途异步回填而不停止轮询：递增纪元后重排一条新纪元的链（等价于 restart） */
  bump(): void
}

export function createPoller(fn: () => Promise<unknown> | unknown, intervalMs: number): Poller {
  // 纪元：每次 start/stop/bump 递增；旧链在执行前后校验纪元，不匹配即终止
  let epoch = 0
  // 是否处于启动状态（stop 后 bump 不应复活轮询链）
  let active = false
  // fn 是否在途：保证单飞
  let running = false
  let timer: number | null = null

  const clearTimer = () => {
    if (timer !== null) {
      window.clearTimeout(timer)
      timer = null
    }
  }

  const schedule = (myEpoch: number) => {
    clearTimer()
    timer = window.setTimeout(() => {
      timer = null
      void run(myEpoch)
    }, intervalMs)
  }

  const run = async (myEpoch: number) => {
    if (myEpoch !== epoch) return
    if (running) {
      // 上一轮 fn 尚未完成（如 bump 重排新链时旧轮仍在途）：跳过本轮，稍后重试
      schedule(myEpoch)
      return
    }
    running = true
    try {
      await fn()
    } catch (error) {
      // 单轮失败不中断轮询链，仅记录
      console.error('轮询执行失败:', error)
    } finally {
      running = false
      // fn 完成后才排下一轮；纪元已变（stop/bump/重复 start）则放弃
      if (myEpoch === epoch) {
        schedule(myEpoch)
      }
    }
  }

  const start = (immediate = false) => {
    epoch++
    active = true
    clearTimer()
    if (immediate) {
      void run(epoch)
    } else {
      schedule(epoch)
    }
  }

  const stop = () => {
    epoch++
    active = false
    clearTimer()
  }

  const bump = () => {
    // 未启动时仅递增纪元作废在途回填；已启动时必须重排新纪元的链，
    // 否则旧链因纪元校验失败而悄然断掉
    if (active) {
      start()
    } else {
      epoch++
    }
  }

  return { start, stop, bump }
}
