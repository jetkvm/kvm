import { useState, useCallback, ReactNode } from 'react';
import clsx from 'clsx';
import { LuGripVertical } from 'react-icons/lu';
import { cva } from "@/cva.config";

interface SortableListProps<T> {
  items: T[];
  keyFn: (item: T) => string;
  onSort: (newItems: T[]) => Promise<void>;
  children: (item: T, index: number) => ReactNode;
  disabled?: boolean;
  className?: string;
  itemClassName?: string;
  variant?: 'list' | 'grid';
  size?: keyof typeof sizes;
  renderHandle?: (isDragging: boolean) => ReactNode;
  hideHandle?: boolean;
  handlePosition?: 'left' | 'right';
}

const sizes = {
  XS: "min-h-[24.5px] py-1 px-3 text-xs",
  SM: "min-h-[32px] py-1.5 px-3 text-[13px]",
  MD: "min-h-[40px] py-2 px-4 text-sm",
  LG: "min-h-[48px] py-2.5 px-4 text-base",
};

const containerVariants = {
  list: {
    XS: 'space-y-1',
    SM: 'space-y-2',
    MD: 'space-y-3',
    LG: 'space-y-4'
  },
  grid: 'grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4',
};

const sortableItemVariants = cva({
  base: 'transition-all duration-300 ease-out rounded',
  variants: {
    size: sizes,
    isDragging: {
      true: 'shadow-lg bg-blue-100/80 dark:bg-blue-900/40 border-blue-200 dark:border-blue-800 z-50',
      false: ''
    },
    isDropTarget: {
      true: 'border border-dashed border-blue-200 dark:border-blue-800 bg-blue-50/50 dark:bg-blue-900/10',
      false: ''
    },
    handlePosition: {
      left: 'flex-row',
      right: 'flex-row-reverse'
    },
    disabled: {
      true: 'pointer-events-none select-none bg-slate-50 text-slate-500/80 dark:bg-slate-800 dark:text-slate-400/80',
      false: 'hover:bg-blue-50/80 active:bg-blue-100/60 dark:hover:bg-slate-700 dark:active:bg-slate-800/60'
    }
  },
  defaultVariants: {
    size: 'MD',
    isDragging: false,
    isDropTarget: false,
    handlePosition: 'left',
    disabled: false
  }
});

const DefaultHandle = ({ isDragging, disabled }: { isDragging: boolean; disabled?: boolean }) => (
  <div className={clsx(
    disabled ? 'cursor-not-allowed' : 'cursor-grab active:cursor-grabbing touch-none',
    'p-1',
    'text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-300',
    isDragging && 'text-slate-700 dark:text-slate-300',
    disabled && 'opacity-50'
  )}>
    <LuGripVertical className="h-4 w-4" />
  </div>
);

export function SortableList<T>({
  items,
  keyFn,
  onSort,
  children,
  disabled = false,
  className = '',
  itemClassName = '',
  variant = 'list',
  size = 'MD',
  renderHandle,
  hideHandle = false,
  handlePosition = 'left',
}: SortableListProps<T>) {
  const [dragItem, setDragItem] = useState<number | null>(null);
  const [dragOverItem, setDragOverItem] = useState<number | null>(null);
  const [touchStartY, setTouchStartY] = useState<number | null>(null);

  const containerClasses = clsx(
    'sortable-list',
    variant === 'grid' ? containerVariants.grid : containerVariants.list[size],
    className
  );

  const getItemClasses = (index: number) => clsx(
    'relative flex items-center',
    sortableItemVariants({ 
      size,
      isDragging: dragItem === index,
      isDropTarget: dragOverItem === index && dragItem !== index,
      handlePosition,
      disabled
    }),
    itemClassName
  );

  const handleDragStart = useCallback((index: number) => {
    if (disabled) return;
    setDragItem(index);
    
    const allItems = document.querySelectorAll('[data-sortable-item]');
    const draggedElement = allItems[index];
    if (draggedElement) {
      draggedElement.classList.add('dragging');
    }
  }, [disabled]);
  
  const handleDragOver = useCallback((e: React.DragEvent, index: number) => {
    if (disabled) return;
    e.preventDefault();
    setDragOverItem(index);
    
    const allItems = document.querySelectorAll('[data-sortable-item]');
    allItems.forEach(el => el.classList.remove('drop-target'));
    
    const targetElement = allItems[index];
    if (targetElement) {
      targetElement.classList.add('drop-target');
    }
  }, [disabled]);
  
  const handleDrop = useCallback(async (e: React.DragEvent) => {
    if (disabled) return;
    e.preventDefault();
    if (dragItem === null || dragOverItem === null) return;
    
    const itemsCopy = [...items];
    const draggedItem = itemsCopy.splice(dragItem, 1)[0];
    itemsCopy.splice(dragOverItem, 0, draggedItem);
    
    await onSort(itemsCopy);
    
    const allItems = document.querySelectorAll('[data-sortable-item]');
    allItems.forEach(el => {
      el.classList.remove('drop-target');
      el.classList.remove('dragging');
    });
    
    setDragItem(null);
    setDragOverItem(null);
  }, [disabled, dragItem, dragOverItem, items, onSort]);

  const handleTouchStart = useCallback((e: React.TouchEvent, index: number) => {
    if (disabled) return;
    const touch = e.touches[0];
    setTouchStartY(touch.clientY);
    setDragItem(index);
    
    const element = e.currentTarget as HTMLElement;
    const rect = element.getBoundingClientRect();
    
    // Create ghost element
    const ghost = element.cloneNode(true) as HTMLElement;
    ghost.id = 'ghost-item';
    ghost.className = 'sortable-ghost';
    ghost.style.height = `${rect.height}px`;
    element.parentNode?.insertBefore(ghost, element);
    
    // Set up dragged element
    element.style.position = 'fixed';
    element.style.left = `${rect.left}px`;
    element.style.top = `${rect.top}px`;
    element.style.width = `${rect.width}px`;
    element.style.zIndex = '50';
  }, [disabled]);

  const handleTouchMove = useCallback((e: React.TouchEvent) => {
    if (disabled || touchStartY === null || dragItem === null) return;
    
    const touch = e.touches[0];
    const deltaY = touch.clientY - touchStartY;
    const element = e.currentTarget as HTMLElement;
    
    element.style.transform = `translateY(${deltaY}px)`;
    
    const sortableElements = document.querySelectorAll('[data-sortable-item]');
    const draggedRect = element.getBoundingClientRect();
    const draggedMiddle = draggedRect.top + draggedRect.height / 2;
    
    sortableElements.forEach((el, i) => {
      if (i === dragItem) return;
      
      const rect = el.getBoundingClientRect();
      const elementMiddle = rect.top + rect.height / 2;
      const distance = Math.abs(draggedMiddle - elementMiddle);
      
      if (distance < rect.height) {
        const direction = draggedMiddle > elementMiddle ? -1 : 1;
        (el as HTMLElement).style.transform = `translateY(${direction * rect.height}px)`;
        (el as HTMLElement).style.transition = 'transform 0.15s ease-out';
      } else {
        (el as HTMLElement).style.transform = '';
        (el as HTMLElement).style.transition = 'transform 0.15s ease-out';
      }
    });
  }, [disabled, touchStartY, dragItem]);

  const handleTouchEnd = useCallback(async (e: React.TouchEvent) => {
    if (disabled || dragItem === null) return;
    
    const element = e.currentTarget as HTMLElement;
    const touch = e.changedTouches[0];
    
    // Remove ghost element
    const ghost = document.getElementById('ghost-item');
    ghost?.parentNode?.removeChild(ghost);
    
    // Reset dragged element styles
    element.style.position = '';
    element.style.left = '';
    element.style.top = '';
    element.style.width = '';
    element.style.zIndex = '';
    element.style.transform = '';
    element.style.transition = '';
    
    const sortableElements = document.querySelectorAll('[data-sortable-item]');
    let targetIndex = dragItem;
    
    // Find the closest element to the final touch position
    const finalY = touch.clientY;
    let closestDistance = Infinity;
    
    sortableElements.forEach((el, i) => {
      if (i === dragItem) return;
      
      const rect = el.getBoundingClientRect();
      const distance = Math.abs(finalY - (rect.top + rect.height / 2));
      
      if (distance < closestDistance) {
        closestDistance = distance;
        targetIndex = i;
      }
      
      // Reset other elements
      (el as HTMLElement).style.transform = '';
      (el as HTMLElement).style.transition = '';
    });
    
    if (targetIndex !== dragItem && closestDistance < 50) {
      const itemsCopy = [...items];
      const [draggedItem] = itemsCopy.splice(dragItem, 1);
      itemsCopy.splice(targetIndex, 0, draggedItem);
      await onSort(itemsCopy);
    }
    
    setTouchStartY(null);
    setDragItem(null);
  }, [disabled, dragItem, items, onSort]);

  return (
    <div className={containerClasses}>
      {items.map((item, index) => (
        <div
          key={keyFn(item)}
          data-sortable-item={index}
          draggable={!disabled}
          onDragStart={() => handleDragStart(index)}
          onDragOver={e => handleDragOver(e, index)}
          onDragEnd={() => {
            const allItems = document.querySelectorAll('[data-sortable-item]');
            allItems.forEach(el => {
              el.classList.remove('drop-target');
              el.classList.remove('dragging');
            });
          }}
          onDrop={handleDrop}
          onTouchStart={e => handleTouchStart(e, index)}
          onTouchMove={handleTouchMove}
          onTouchEnd={handleTouchEnd}
          className={getItemClasses(index)}
        >
          <div className="flex-1 min-w-0 flex items-center gap-2">
            {handlePosition === 'left' && !hideHandle && (
              renderHandle ? 
                renderHandle(dragItem === index) : 
                <DefaultHandle isDragging={dragItem === index} disabled={disabled} />
            )}
            <div className="flex-1 min-w-0">
              {children(item, index)}
            </div>
            {handlePosition === 'right' && !hideHandle && (
              renderHandle ? 
                renderHandle(dragItem === index) : 
                <DefaultHandle isDragging={dragItem === index} disabled={disabled} />
            )}
          </div>
        </div>
      ))}
    </div>
  );
} 